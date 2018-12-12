package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"text/template"

	"github.com/CCI-MOC/hil-vpn/internal/staticconfig"
)

const configDir = "/etc/openvpn/server"

// Template for the open vpn config files we generate.
var openVpnCfgTpl = template.Must(template.New("openvpn-config").Parse(`
# This file is automatically generated by hil-vpn-privop; do not modify manually.

dev tap{{ .NewInterfaceName }}
secret hil-vpn-{{ .Name }}.key

# The default cipher is insecure, so we explicitly set the cipher to the openvpn
# project's recommendation. See https://community.openvpn.net/openvpn/wiki/SWEET32
cipher AES-256-CBC

lport {{ .Port }}

up "{{ .Libexecdir }}/hil-vpn-hook-up {{ .Vlan }}"
# Needed to permit the above to actually run:
script-security 2

user nobody
group nobody
`))

type OpenVpnCfg struct {
	Name string
	Key  string
	Port uint16
	Vlan uint16
}

type templateArg struct {
	OpenVpnCfg
	Libexecdir string
}

// Get the path to the file in which to store the openvpn config for the
// named vpn.
func getCfgPath(name string) string {
	return configDir + "/" + name + ".conf"
}

// Get the path to the file in which to store the key for the named vpn.
func getKeyPath(name string) string {
	return configDir + "/hil-vpn-" + name + ".key"
}

// Get the name of the systemd service for the named vpn.
func getServiceName(vpnName string) string {
	return "openvpn-server@" + vpnName
}

// Save the openvpn config and its static keys to disk.
func (cfg OpenVpnCfg) Save() error {
	cfgPath := getCfgPath(cfg.Name)
	keyPath := getKeyPath(cfg.Name)

	cfgFile, err := os.OpenFile(cfgPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		cfgFile.Close()
		if err != nil {
			os.Remove(cfgPath)
		}
	}()
	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		keyFile.Close()
		if err != nil {
			os.Remove(keyPath)
		}
	}()
	arg := templateArg{
		OpenVpnCfg: cfg,
		Libexecdir: staticconfig.Libexecdir,
	}
	if err = openVpnCfgTpl.Execute(cfgFile, arg); err != nil {
		return err
	}
	_, err = keyFile.Write([]byte(cfg.Key))
	return err
}

// Return a cryptographically-random 12-character base64(url) encoded string.
// This is to do collision avoidance given the 15-character limit on network
// interface names. See also issue #14. We still prefix interface names with
// tap for two reasons:
//
// 1. A modicum of readability.
// 2. So that openvpn can infer the type of device. We could also deal with
//    this by setting `dev-type tap` in the config file.
//
// Note that 12 bytes of base64 (which is about 9 bytes decoded) is not in
// general a reasonable amount of entropy for cryptographic purposes. We'll
// settle for it in this case because:
//
// 1. The value needn't be secret, just collision avoidant.
// 2. The failure case is very mild: if a user is already able to invoke
//    hil-vpn-privop as root, they can cause two networks to try to share
//    the same interface; the consequence of this is that only one of them
//    will start. At this point the user already has the authority to destroy
//    newtorks and grant access to arbitrary vlans, so... whoopdy-do.
func (cfg OpenVpnCfg) NewInterfaceName() string {
	var data [16]byte
	_, err := rand.Read(data[:])
	chkfatal("Generating interface name", err)
	return base64.RawURLEncoding.EncodeToString(data[:])[:12]
}

// Generate a new openvpn config (including a static key).
func NewOpenVpnConfig(name string, vlan, port uint16) (*OpenVpnCfg, error) {
	cmd := exec.Command("openvpn", "--genkey", "--secret", "/dev/fd/1")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Error invoking openvpn: %v", err)
	}
	return &OpenVpnCfg{
		Name: name,
		Port: port,
		Vlan: vlan,
		Key:  string(output),
	}, nil
}
