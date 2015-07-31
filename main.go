package main

import (
	"flag"
	"log"
	"cni-docker-plugin/driver"
)

const (
	Version = "0.0"
)

func main() {
	var (
		socket	string
		debug	bool
		plugpath string
		netconfpath string
		d	driver.Driver
	)

	flag.BoolVar(&debug, "debug", false, "output debugging info to stderr")
	flag.StringVar(&socket, "socket", "/usr/share/docker/plugins/cni.sock", "socket on which to listen")
	flag.StringVar(&plugpath, "plugpath", "/usr/libexec/cni-plugins", "path to CNI executables")
	flag.StringVar(&netconfpath, "netconfpath", "/etc/cni/net.d", "path to CNI network configuration files")
	flag.Parse()

	d, err := driver.New(Version, plugpath, netconfpath)
	if err != nil {
		log.Fatalf("Failed to create driver: %s", err)
	}

	if err := d.Listen(socket); err != nil {
		log.Fatal(err)
	}
}
