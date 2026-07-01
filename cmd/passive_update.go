package cmd

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/settings"
	"github.com/autonoco/buttons/internal/updater"
	"github.com/spf13/cobra"
)

const passiveUpdateThrottle = 6 * time.Hour

func maybeRunPassiveUpdate(cmd *cobra.Command) {
	if isUpdateCommand(cmd) || passiveUpdatesDisabledByEnv() {
		return
	}
	if shouldSkipPassiveUpdate() {
		return
	}
	opts := updater.Options{
		CurrentVersion: version,
		RegistryURL:    registryURL(),
		RegistryKey:    registryKey(),
		Writer:         io.Discard,
	}
	force := forcedPassiveUpdateRequired(opts)
	svc, err := settings.NewServiceFromEnv()
	if err != nil {
		return
	}
	st, err := svc.Load()
	if err != nil || (!force && !st.AutoUpdateEnabled()) {
		return
	}
	now := time.Now()
	last := st.LastUpdateCheckUnixOrZero()
	if !force && last > 0 && now.Sub(time.Unix(last, 0)) < passiveUpdateThrottle {
		return
	}
	if err := svc.SetLastUpdateCheckUnix(now.Unix()); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	_, _ = updater.Apply(ctx, opts)
}

func shouldSkipPassiveUpdate() bool {
	if os.Getenv("CI") != "" {
		return true
	}
	return config.IsNonTTY()
}

func forcedPassiveUpdateRequired(opts updater.Options) bool {
	if opts.RegistryURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return updater.ForceCLIUpdateRequired(ctx, opts)
}

func isUpdateCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "update" {
			return true
		}
	}
	return false
}

func passiveUpdatesDisabledByEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("BUTTONS_NO_UPDATE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
