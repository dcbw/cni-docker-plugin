package driver

import (
	"log"
	"fmt"

	docker "github.com/dcbw/go-dockerclient"
)

type watcher struct {
	dockerer
	networks map[string]*docker.Network  // id :: network info
	containers map[string]*docker.Container
	events   chan *docker.APIEvents
}

type Watcher interface {
	WatchNetwork(nw *docker.Network)
	UnwatchNetwork(id string)
	GetNetworkById(id string) *docker.Network
	GetContainerBySandboxKey(sandbox string) *docker.Container
	GetContainerNetns(id string) (string, error)
}

func NewWatcher(client *docker.Client) (Watcher, error) {
	w := &watcher{
		dockerer: dockerer{
			client: client,
		},
		networks: make(map[string]*docker.Network),
		containers: make(map[string]*docker.Container),
		events:   make(chan *docker.APIEvents),
	}
	err := client.AddEventListener(w.events)
	if err != nil {
		return nil, err
	}

	networks, err := client.ListNetworks()
	if err != nil {
		return nil, err
	}
	for _, nw := range networks {
		w.WatchNetwork(&nw)
	}

	go func() {
		for event := range w.events {
			switch event.Status {
			case "start":
				w.ContainerStart(event.ID)
			case "die":
				w.ContainerDied(event.ID)
			case "create":
				w.ContainerStart(event.ID)
			default:
				log.Printf("Event %+v", event);
			}
		}
	}()

	return w, nil
}

func (w *watcher) WatchNetwork(nw *docker.Network) {
	log.Printf("Watch network %s (%s)", nw.ID, nw.Name)
	w.networks[nw.ID] = nw
}

func (w *watcher) GetNetworkById(id string) *docker.Network {
	return w.networks[id]
}

func (w *watcher) UnwatchNetwork(id string) {
	log.Printf("Unwatch network %s", id)
	delete(w.networks, id)
}

func (w *watcher) ContainerStart(id string) {
	log.Printf("Container started %s", id)
	container, err := w.InspectContainer(id)
	log.Printf("container: %+v", container.NetworkSettings)
	if err != nil {
		log.Printf("error inspecting container: %s", err)
		return
	}
	w.containers[id] = container
}

func (w *watcher) ContainerDied(id string) {
	log.Printf("Container died %s", id)
	_, err := w.InspectContainer(id)
	if err != nil {
		log.Printf("error inspecting container: %s", err)
		return
	}
	delete(w.containers, id)
}

func (w *watcher) GetContainerBySandboxKey(sandbox string) *docker.Container {
	for _, container := range w.containers {
		if container.NetworkSettings.SandboxKey == sandbox {
			return container
		}
	}
	return nil
}

func (w *watcher) GetContainerNetns(id string) (string, error) {
	container, ok := w.containers[id]
	if !ok {
		return "", fmt.Errorf("Container %s not found", id)
	}
	pid := container.State.Pid
	if pid <= 0 {
		return "", fmt.Errorf("Container %s not running", id)
	}
	return fmt.Sprintf("/proc/%s/ns/net", pid), nil
}
