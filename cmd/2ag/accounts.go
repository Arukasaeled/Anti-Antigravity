package main

import (
	"encoding/json"
	"fmt"
	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/supervisor"
	"os"
)

// No Manager window is opened or closed. Account mutations share its lock.
func accountCommand(cfg config.Config, args []string) error {
	supervisor.SetRuntimeMode(cfg.RuntimeMode)
	if len(args) == 1 && args[0] == "status" {
		email, err := supervisor.ReadHostLoginEmail()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"email": email, "native_auth": supervisor.ReadNativeAuthState(), "vault": supervisor.ListVaultAccountEntries()})
	}
	if len(args) == 2 && args[0] == "switch" {
		result, err := supervisor.SwitchAccountTransactional(args[1], cfg.RuntimeMode)
		json.NewEncoder(os.Stdout).Encode(result)
		return err
	}
	return fmt.Errorf("usage: 2ag account status | 2ag account switch <email>")
}
