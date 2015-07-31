package driver

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"bytes"
	"strings"

	docker "github.com/dcbw/go-dockerclient"
	"github.com/gorilla/mux"
)

const (
	MethodReceiver = "NetworkDriver"
)

type Driver interface {
	Listen(string) error
}

type driver struct {
	dockerer
	version     string
	plugpath    string
	netconfpath string
	watcher     Watcher
}

func New(version string, plugpath string, netconfpath string) (Driver, error) {
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return nil, fmt.Errorf("could not connect to docker: %s", err)
	}

	watcher, err := NewWatcher(client)
	if err != nil {
		return nil, err
	}

	return &driver{
		dockerer: dockerer{
			client: client,
		},
		version: version,
		plugpath: plugpath,
		netconfpath: netconfpath,
		watcher: watcher,
	}, nil
}

func (driver *driver) Listen(socket string) error {
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(notFound)

	router.Methods("GET").Path("/status").HandlerFunc(driver.status)
	router.Methods("POST").Path("/Plugin.Activate").HandlerFunc(driver.handshake)

	handleMethod := func(method string, h http.HandlerFunc) {
		router.Methods("POST").Path(fmt.Sprintf("/%s.%s", MethodReceiver, method)).HandlerFunc(h)
	}

	handleMethod("CreateNetwork", driver.createNetwork)
	handleMethod("DeleteNetwork", driver.deleteNetwork)
	handleMethod("CreateEndpoint", driver.createEndpoint)
	handleMethod("DeleteEndpoint", driver.deleteEndpoint)
	handleMethod("EndpointOperInfo", driver.infoEndpoint)
	handleMethod("Join", driver.joinEndpoint)
	handleMethod("Leave", driver.leaveEndpoint)

	var (
		listener net.Listener
		err      error
	)

	listener, err = net.Listen("unix", socket)
	if err != nil {
		return err
	}

	s := &http.Server{
		Handler: router,
	}
	s.SetKeepAlivesEnabled(false)
	return s.Serve(listener)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	log.Printf("[plugin] Not found: %+v", r)
	http.NotFound(w, r)
}

func sendError(w http.ResponseWriter, msg string, code int) {
	log.Printf("%d %s", code, msg)
	http.Error(w, msg, code)
}

func errorResponsef(w http.ResponseWriter, fmtString string, item ...interface{}) {
	json.NewEncoder(w).Encode(map[string]string{
		"Err": fmt.Sprintf(fmtString, item...),
	})
}

func objectResponse(w http.ResponseWriter, obj interface{}) {
	if err := json.NewEncoder(w).Encode(obj); err != nil {
		sendError(w, "Could not JSON encode response", http.StatusInternalServerError)
		return
	}
}

func emptyResponse(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]string{})
}

// === protocol handlers

type handshakeResp struct {
	Implements []string
}

func (driver *driver) handshake(w http.ResponseWriter, r *http.Request) {
	err := json.NewEncoder(w).Encode(&handshakeResp{
		[]string{"NetworkDriver"},
	})
	if err != nil {
		log.Fatal("handshake encode:", err)
		sendError(w, "encode error", http.StatusInternalServerError)
		return
	}
	log.Printf("Handshake completed")
}

func (driver *driver) status(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintln("CNI plugin", driver.version))
}

type networkCreate struct {
	NetworkID string
	Options   map[string]interface{}
}

// CNM's CreateNetwork request has no analogue in CNI, so we simply
// track the network so we can fetch its name
func (driver *driver) createNetwork(w http.ResponseWriter, r *http.Request) {
	var create networkCreate
	err := json.NewDecoder(r.Body).Decode(&create)
	if err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Create network request %+v", &create)

	emptyResponse(w)

	// Retrieve the network name from Docker after the response
	// has been sent and the connection has closed (the network doesn't
	// exist in docker until close)
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		nw, err := driver.NetworkInfo(create.NetworkID)
		if err != nil {
			log.Printf("NetworkInfo error %+v", err)
		} else {
			log.Printf("Watching network %+v", nw)
			driver.watcher.WatchNetwork(nw)
		}
	}()
}

type networkDelete struct {
	NetworkID string
}

func (driver *driver) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	var delete networkDelete
	if err := json.NewDecoder(r.Body).Decode(&delete); err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Delete network request: %+v", &delete)

	driver.watcher.UnwatchNetwork(delete.NetworkID)
	emptyResponse(w)
	log.Printf("Destroy network %s", delete.NetworkID)
}

type endpointCreate struct {
	NetworkID  string
	EndpointID string
	Interfaces []*iface
	Options    map[string]interface{}
}

type iface struct {
	ID         int
	SrcName    string
	DstPrefix  string
	Address    string
	MacAddress string
}

type endpointResponse struct {
	Interfaces []*iface
}

// CNM's CreateEndpoint request loosely maps to CNI's IPAM ADD action, but CNI
// rolls the IPAM stuff into the ADD process of the network plugin.  So we
// can't do anything here.
func (driver *driver) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var create endpointCreate
	if err := json.NewDecoder(r.Body).Decode(&create); err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Create endpoint request %+v", &create)
	endID := create.EndpointID

	resp := &endpointResponse{
		Interfaces: []*iface{},
	}

	objectResponse(w, resp)
	log.Printf("Create endpoint %s %+v", endID, resp)
}

type endpointDelete struct {
	NetworkID  string
	EndpointID string
}

func (driver *driver) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	var delete endpointDelete
	if err := json.NewDecoder(r.Body).Decode(&delete); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Printf("Delete endpoint request: %+v", &delete)
	emptyResponse(w)

	log.Printf("Delete endpoint %s", delete.EndpointID)
}

type endpointInfoReq struct {
	NetworkID  string
	EndpointID string
}

type endpointInfo struct {
	Value map[string]interface{}
}

func (driver *driver) infoEndpoint(w http.ResponseWriter, r *http.Request) {
	var info endpointInfoReq
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Printf("Endpoint info request: %+v", &info)
	objectResponse(w, &endpointInfo{Value: map[string]interface{}{}})
	log.Printf("Endpoint info %s", info.EndpointID)
}

type joinInfo struct {
	InterfaceNames []*iface
	Gateway        string
	GatewayIPv6    string
	HostsPath      string
	ResolvConfPath string
}

type join struct {
	NetworkID  string
	EndpointID string
	SandboxKey string
	Options    map[string]interface{}
}

type staticRoute struct {
	Destination string
	RouteType   int
	NextHop     string
	InterfaceID int
}

type joinResponse struct {
	HostsPath      string
	ResolvConfPath string
	Gateway        string
	InterfaceNames []*iface
	StaticRoutes   []*staticRoute
}

func envVars(vars [][2]string) []string {
	env := os.Environ()

	for _, kv := range vars {
		env = append(env, strings.Join(kv[:], "="))
	}

	return env
}

func (driver *driver) execPlugin(plugin string, cmd string, containerid string, netns string, config string) ([]byte, error) {
	fullname := filepath.Join(driver.plugpath, plugin)
	if fi, err := os.Stat(fullname); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("Failed to find plugin name %s/%s", driver.plugpath, plugin)
	}

	vars := [][2]string{
		{"CNI_COMMAND", cmd},
		{"CNI_CONTAINERID", containerid},
		{"CNI_NETNS", netns},
		{"CNI_PATH", driver.plugpath},
	}

	stdin := bytes.NewBuffer([]byte(config))
	stdout := &bytes.Buffer{}

	c := exec.Cmd{
		Path:   fullname,
		Args:   []string{fullname},
		Env:    envVars(vars),
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: os.Stderr,
	}

	err := c.Run()
	return stdout.Bytes(), err
}

// Here's where everything happens for CNI.  We call the CNI plugins
// with some constructed network information.
//
// CNI_COMMAND: indicates the desired operation; either ADD or DEL
// CNI_CONTAINERID: Container ID
// CNI_NETNS: Path to network namespace file
// CNI_IFNAME: Interface name to set up
// CNI_ARGS: Extra arguments passed in by the user at invocation time. Alphanumeric key-value pairs separated by semicolons; for example, "FOO=BAR;ABC=123"
// CNI_PATH: Colon-separated list of paths to search for CNI plugin executables
//
func (driver *driver) joinEndpoint(w http.ResponseWriter, r *http.Request) {
	var j join
	if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Printf("Join request: %+v", &j)

	// Get network name here
	nw := driver.watcher.GetNetworkById(j.NetworkID)
	if nw == nil {
		sendError(w, "Could not find requested network to join", http.StatusInternalServerError)
		return
	}

	container := driver.watcher.GetContainerBySandboxKey(j.SandboxKey)
	if container == nil {
		sendError(w, fmt.Sprintf("Failed to find container with sandbox %s", j.SandboxKey), http.StatusInternalServerError)
		return
	}

	// Get the network namespace path
	netns, err := driver.watcher.GetContainerNetns(container.ID)
	if err != nil {
		sendError(w, fmt.Sprintf("Failed to find container %s netns", container.ID), http.StatusInternalServerError)
		return
	}

	output, err := driver.execPlugin(nw.Type, "ADD", j.SandboxKey, netns, "")
	if err != nil {
		sendError(w, fmt.Sprintf("Plugin %s failed the ADD operation: %v", nw.Type, err), http.StatusInternalServerError)
		return
	}
	log.Printf("Join plugin %s output: %s", nw.Type, output)

	ifname := &iface{
		SrcName:   "blahblah",
		DstPrefix: "ethwe",
		ID:        0,
	}

	res := &joinResponse{
		InterfaceNames: []*iface{ifname},
	}

	objectResponse(w, res)
	log.Printf("Join endpoint %s:%s to %s", j.NetworkID, j.EndpointID, j.SandboxKey)
}

type leave struct {
	NetworkID  string
	EndpointID string
	Options    map[string]interface{}
}

func (driver *driver) leaveEndpoint(w http.ResponseWriter, r *http.Request) {
	var l leave
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Printf("Leave request: %+v", &l)

	emptyResponse(w)
	log.Printf("Leave %s:%s", l.NetworkID, l.EndpointID)
}

// ===

func makeMac(ip net.IP) string {
	hw := make(net.HardwareAddr, 6)
	hw[0] = 0x7a
	hw[1] = 0x42
	copy(hw[2:], ip.To4())
	return hw.String()
}
