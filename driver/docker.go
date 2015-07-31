package driver

import (
	"log"
	"github.com/dcbw/go-dockerclient"
)

type dockerer struct {
	client *docker.Client
}

func (d *dockerer) getContainerBridgeIP(nameOrID string) (string, error) {
	log.Printf("Getting IP for container %s", nameOrID)
	info, err := d.InspectContainer(nameOrID)
	if err != nil {
		return "", err
	}
	return info.NetworkSettings.IPAddress, nil
}

func (d *dockerer) InspectContainer(nameOrId string) (*docker.Container, error) {
	return d.client.InspectContainer(nameOrId)
}

func (d *dockerer) NetworkInfo(id string) (*docker.Network, error) {
	return d.client.NetworkInfo(id)
}

func (d *dockerer) ListNetworks() ([]docker.Network, error) {
	return d.client.ListNetworks()
}

