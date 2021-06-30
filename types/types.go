// Copyright 2020 Nokia
// Licensed under the BSD 3-Clause License.
// SPDX-License-Identifier: BSD-3-Clause

package types

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cloudflare/cfssl/log"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/docker/go-connections/nat"
	"github.com/srl-labs/containerlab/utils"
)

// Link is a struct that contains the information of a link between 2 containers
type Link struct {
	A      *Endpoint
	B      *Endpoint
	MTU    int
	Labels map[string]string
	Vars   map[string]string
}

func (link *Link) String() string {
	return fmt.Sprintf("link [%s:%s, %s:%s]", link.A.Node.ShortName,
		link.A.EndpointName, link.B.Node.ShortName, link.B.EndpointName)
}

// Endpoint is a struct that contains information of a link endpoint
type Endpoint struct {
	Node *NodeConfig
	// e1-x, eth, etc
	EndpointName string
	// mac address
	MAC string
}

// mgmtNet struct defines the management network options
// it is provided via docker network object
type MgmtNet struct {
	Network    string `yaml:"network,omitempty"` // docker network name
	Bridge     string `yaml:"bridge,omitempty"`  // linux bridge backing the docker network (or containerd bridge net)
	IPv4Subnet string `yaml:"ipv4_subnet,omitempty"`
	IPv6Subnet string `yaml:"ipv6_subnet,omitempty"`
	MTU        string `yaml:"mtu,omitempty"`
}

// NodeConfig is a struct that contains the information of a container element
type NodeConfig struct {
	ShortName        string
	LongName         string
	Fqdn             string
	LabDir           string // LabDir is a directory related to the node, it contains config items and/or other persistent state
	Index            int
	Group            string
	Kind             string
	StartupConfig    string // path to config template file that is used for startup config generation
	ResStartupConfig string // path to config file that is actually mounted to the container and is a result of templation
	Config           *ConfigDispatcher
	NodeType         string
	Position         string
	License          string
	Image            string
	Sysctls          map[string]string
	User             string
	Entrypoint       string
	Cmd              string
	Env              map[string]string
	Binds            []string    // Bind mounts strings (src:dest:options)
	PortBindings     nat.PortMap // PortBindings define the bindings between the container ports and host ports
	PortSet          nat.PortSet // PortSet define the ports that should be exposed on a container
	// container networking mode. if set to `host` the host networking will be used for this node, else bridged network
	NetworkMode          string
	MgmtNet              string // name of the docker network this node is connected to with its first interface
	MgmtIPv4Address      string
	MgmtIPv4PrefixLength int
	MgmtIPv6Address      string
	MgmtIPv6PrefixLength int
	MacAddress           string
	ContainerID          string
	TLSCert              string
	TLSKey               string
	TLSAnchor            string
	NSPath               string   // network namespace path for this node
	Publish              []string //list of ports to publish with mysocketctl
	// container labels
	Labels map[string]string
	// Slice of pointers to local endpoints
	Endpoints []*Endpoint
}

// GenerateConfig generates configuration for the nodes
// out of the templ based on the node configuration and saves the result to dst
func (node *NodeConfig) GenerateConfig(dst, templ string) error {
	// if startup config is not set, and the config file is already present in the node dir
	// we do not regenerate the config, since we will take what was saved from the previous run
	// in other words, the startup config set by a user takes preference and will trigger config generation
	if utils.FileExists(dst) && (node.StartupConfig == "") {
		log.Debugf("config file '%s' for node '%s' already exists and will not be generated", dst, node.ShortName)
		return nil
	}
	log.Debugf("generating config for node %s from file %s", node.ShortName, node.StartupConfig)
	tpl, err := template.New(filepath.Base(node.StartupConfig)).Parse(templ)
	if err != nil {
		return err
	}
	dstBytes := new(bytes.Buffer)
	err = tpl.Execute(dstBytes, node)
	if err != nil {
		return err
	}
	log.Debugf("node '%s' generated config: %s", node.ShortName, dstBytes.String())
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(dstBytes.Bytes())
	return err
}

func DisableTxOffload(n *NodeConfig) error {
	// skip this if node runs in host mode
	if strings.ToLower(n.NetworkMode) == "host" {
		return nil
	}
	// disable tx checksum offload for linux containers on eth0 interfaces
	nodeNS, err := ns.GetNS(n.NSPath)
	if err != nil {
		return err
	}
	err = nodeNS.Do(func(_ ns.NetNS) error {
		// disabling offload on lo0 interface
		err := utils.EthtoolTXOff("eth0")
		if err != nil {
			log.Infof("Failed to disable TX checksum offload for 'eth0' interface for Linux '%s' node: %v", n.ShortName, err)
		}
		return err
	})
	return err
}

// Data struct storing generic container data
type GenericContainer struct {
	Names           []string
	ID              string
	ShortID         string // trimmed ID for display purposes
	Image           string
	State           string
	Status          string
	Labels          map[string]string
	Pid             int
	NetworkSettings *GenericMgmtIPs
}

type GenericMgmtIPs struct {
	Set      bool
	IPv4addr string
	IPv4pLen int
	IPv6addr string
	IPv6pLen int
}

type GenericFilter struct {
	// defined by now "label"
	FilterType string
	// defines e.g. the label name for FilterType "label"
	Field string
	// = | != | exists
	Operator string
	// match value
	Match string
}

func FilterFromLabelStrings(labels []string) []*GenericFilter {
	gfl := []*GenericFilter{}
	var gf *GenericFilter
	for _, s := range labels {
		gf = &GenericFilter{
			FilterType: "label",
		}
		if strings.Contains(s, "=") {
			gf.Operator = "="
			subs := strings.Split(s, "=")
			gf.Field = strings.TrimSpace(subs[0])
			gf.Match = strings.TrimSpace(subs[1])
		} else {
			gf.Match = "exists"
			gf.Field = strings.TrimSpace(s)
		}

		gfl = append(gfl, gf)
	}
	return gfl
}

// ConfigDispatcher represents the config of a configuration machine
// that is responsible to execute configuration commands on the nodes
// after they started
type ConfigDispatcher struct {
	Vars map[string]string `yaml:"vars,omitempty"`
}
