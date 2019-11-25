package main

import (
	"encoding/json"
	"fmt"
	"github.com/Kong/go-pdk"
)

// --- instanceData --- //
type instanceData struct {
	id          int
	plugin      *pluginData
	initialized bool
	config      interface{}
	handlers    map[string]func(kong *pdk.PDK)
}

type (
	certificater interface{ Certificate(*pdk.PDK) }
	rewriter     interface{ Rewrite(*pdk.PDK) }
	accesser     interface{ Access(*pdk.PDK) }
	headerFilter interface{ HeaderFilter(*pdk.PDK) }
	bodyFilter   interface{ BodyFilter(*pdk.PDK) }
	prereader    interface{ Preread(*pdk.PDK) }
	logger       interface{ Log(*pdk.PDK) }
)

func getHandlers(config interface{}) map[string]func(kong *pdk.PDK) {
	handlers := map[string]func(kong *pdk.PDK){}

	if h, ok := config.(certificater); ok { handlers["certificate"]   = h.Certificate  }
	if h, ok := config.(rewriter)    ; ok { handlers["rewrite"]       = h.Rewrite      }
	if h, ok := config.(accesser)    ; ok { handlers["access"]        = h.Access       }
	if h, ok := config.(headerFilter); ok { handlers["header_filter"] = h.HeaderFilter }
	if h, ok := config.(bodyFilter)  ; ok { handlers["body_filter"]   = h.BodyFilter   }
	if h, ok := config.(prereader)   ; ok { handlers["preread"]       = h.Preread      }
	if h, ok := config.(logger)      ; ok { handlers["log"]           = h.Log          }

	return handlers
}

// Configuration data for a new plugin instance.
type PluginConfig struct {
	Name   string // plugin name
	Config []byte // configuration data, as a JSON string
}

// Current state of a plugin instance.  TODO: add some statistics
type InstanceStatus struct {
	Name   string      // plugin name
	Id     int         // instance id
	Config interface{} // configuration data, decoded
}

// StartInstance starts a plugin instance, as requred by configuration data.  More than
// one instance can be started for a single plugin.  If the configuration changes,
// a new instance should be started and the old one closed.
//
// RPC exported method
func (s *PluginServer) StartInstance(config PluginConfig, status *InstanceStatus) error {
	plug, err := s.loadPlugin(config.Name)
	if err != nil {
		return err
	}

	instanceConfig := plug.constructor()

	if err := json.Unmarshal(config.Config, instanceConfig); err != nil {
		return fmt.Errorf("Decoding config: %w", err)
	}

	instance := instanceData{
		plugin:   plug,
		config:   instanceConfig,
		handlers: getHandlers(instanceConfig),
	}

	s.lock.Lock()
	instance.id = s.nextInstanceId
	s.nextInstanceId++
	s.instances[instance.id] = &instance
	s.lock.Unlock()

	*status = InstanceStatus{
		Name:   config.Name,
		Id:     instance.id,
		Config: instance.config,
	}

	return nil
}

// InstanceStatus returns a given resource's status (the same given when started)
//
// RPC exported method
func (s *PluginServer) InstanceStatus(id int, status *InstanceStatus) error {
	s.lock.RLock()
	instance, ok := s.instances[id]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No plugin instance %d", id)
	}

	*status = InstanceStatus{
		Name:   instance.plugin.name,
		Id:     instance.id,
		Config: instance.config,
	}

	return nil
}

// CloseInstance is used when an instance shouldn't be used anymore.
// Doesn't kill any running event but the instance is no longer accesible,
// so it's not possible to start a new event with it and will be garbage
// collected after the last reference event finishes.
// Returns the status just before closing.
//
// RPC exported method
func (s *PluginServer) CloseInstance(id int, status *InstanceStatus) error {
	s.lock.RLock()
	instance, ok := s.instances[id]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No plugin instance %d", id)
	}

	*status = InstanceStatus{
		Name:   instance.plugin.name,
		Id:     instance.id,
		Config: instance.config,
	}

	// kill?

	s.lock.Lock()
	delete(s.instances, id)
	s.lock.Unlock()

	return nil
}
