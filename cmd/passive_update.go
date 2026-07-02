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
	if shouldSkipPassiveUpdateFunc() {
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
	if err != nil {
		return
	}
	opts.CLIAutoUpdate = st.CLIAutoUpdateEnabled() || force
	plan := passiveUpdatePlan(st, force, time.Now())
	if !plan.run {
		return
	}
	opts.SkipBinary = plan.skipBinary
	opts.SkipContent = plan.skipContent
	if plan.recordCheck {
		if err := svc.SetLastUpdateCheckUnix(plan.now.Unix()); err != nil {
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	_, _ = updater.Apply(ctx, opts)
}

type passiveUpdateDecision struct {
	run         bool
	skipBinary  bool
	skipContent bool
	recordCheck bool
	now         time.Time
}

func passiveUpdatePlan(st *settings.Settings, force bool, now time.Time) passiveUpdateDecision {
	if st == nil {
		return passiveUpdateDecision{}
	}
	buttonsEnabled := st.ButtonsAutoUpdateEnabled()
	cliEnabled := st.CLIAutoUpdateEnabled() || force
	if !buttonsEnabled && !cliEnabled {
		return passiveUpdateDecision{}
	}
	decision := passiveUpdateDecision{
		run:         true,
		skipBinary:  !cliEnabled,
		skipContent: !buttonsEnabled,
		now:         now,
	}
	if !cliEnabled {
		return decision
	}
	if force {
		decision.recordCheck = true
		return decision
	}
	last := st.LastUpdateCheckUnixOrZero()
	if last > 0 && now.Sub(time.Unix(last, 0)) < passiveUpdateThrottle {
		decision.skipBinary = true
		if decision.skipContent {
			decision.run = false
		}
		return decision
	}
	decision.recordCheck = true
	return decision
}

var shouldSkipPassiveUpdateFunc = shouldSkipPassiveUpdate

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
