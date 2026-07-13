package clone

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func Run(ctx context.Context, cfg *config.Config, logger *logx.Logger) error {
	logger.Infof("%s", cfg.Summary())
	reader := bufio.NewReader(os.Stdin)
	if !cfg.Yes {
		if cfg.Quiet {
			return fmt.Errorf("--quiet requires --yes")
		}
		if !confirmExecution(reader, "\nContinue? [y/N]: ") {
			logger.Infof("[USER ABORT]")
			return nil
		}
	}
	if cfg.InitializeFull {
		if !confirmExecution(reader, "FULL INITIALIZE deletes all deletable settings. Continue? [y/N]: ") {
			logger.Infof("[USER ABORT]")
			return nil
		}
	}
	logger.Infof("[START] %s", model.ZabbixTime())
	engine, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer engine.Close()
	if cfg.DryRun {
		defer logDryRunOperations(logger, engine.API)
	}
	if err = engine.FirstProcess(ctx); err != nil {
		return fmt.Errorf("firstProcess: %w", err)
	}
	if engine.IsMaster() {
		if err = engine.CreateMasterData(ctx); err != nil {
			return fmt.Errorf("createNewData: %w", err)
		}
		if err = engine.SaveMasterData(ctx); err != nil {
			return fmt.Errorf("setVersionDataToStore: %w", err)
		}
	} else {
		if err = engine.ChangePassword(ctx); err != nil {
			return fmt.Errorf("changePassword: %w", err)
		}
		var direct *Engine
		if cfg.StoreType == "direct" {
			directCfg := cfg.DirectMaster()
			direct, err = New(ctx, directCfg, logger)
			if err != nil {
				return err
			}
			defer direct.Close()
		}
		if err = engine.LoadReplicaData(ctx, direct); err != nil {
			return fmt.Errorf("getDataFromStore: %w", err)
		}
		if err = engine.ApplyGlobalSettings(ctx); err != nil {
			return fmt.Errorf("setGlobalsettingsToZabbix: %w", err)
		}
		if err = engine.ApplyAPISection(ctx, "PRE"); err != nil {
			return fmt.Errorf("setApiToZabbix PRE: %w", err)
		}
		if err = engine.ApplyConfiguration(ctx); err != nil {
			return fmt.Errorf("setConfigurationToZabbix: %w", err)
		}
		if engine.Config.Online {
			logger.Infof("Set AlertStop in Update: SKIP (--online).")
		} else if err = engine.SetAlertStop(ctx); err != nil {
			return fmt.Errorf("setAlertStopInUpdate: %w", err)
		}
		if err = engine.ApplyAPISection(ctx, "MID"); err != nil {
			return fmt.Errorf("setApiToZabbix MID: %w", err)
		}
		if err = engine.ApplyHosts(ctx); err != nil {
			return fmt.Errorf("setHostToZabbix: %w", err)
		}
		if err = engine.ExecuteCheckNow(ctx); err != nil {
			return fmt.Errorf("execCheckNow: %w", err)
		}
		for _, section := range []string{"POST", "ACCOUNT", "EXTEND"} {
			if err = engine.ApplyAPISection(ctx, section); err != nil {
				return fmt.Errorf("setApiToZabbix %s: %w", section, err)
			}
		}
		if err = engine.ApplyDeferredSettings(ctx); err != nil {
			return fmt.Errorf("setGlobalsettingsToZabbix deferred: %w", err)
		}
		if err = engine.ApplyAuthentication(ctx); err != nil {
			return fmt.Errorf("setAuthenticationToZabbix: %w", err)
		}
		if err = engine.ApplyAlertMedia(ctx); err != nil {
			return fmt.Errorf("setAlertMedia: %w", err)
		}
		if _, err = engine.ApplyPSK(ctx); err != nil {
			return fmt.Errorf("setPskToZabbix: %w", err)
		}
	}
	if err = engine.SetVersionCode(ctx, false); err != nil {
		return fmt.Errorf("setVersionCode: %w", err)
	}
	logger.Infof("[FINISH] %s", model.ZabbixTime())
	return nil
}

func confirmExecution(reader *bufio.Reader, prompt string) bool {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	answer := strings.ToUpper(strings.TrimSpace(line))
	return answer == "Y" || answer == "YES"
}

func logDryRunOperations(logger *logx.Logger, api *zabbix.Client) {
	counts := api.DryRunMethods()
	if len(counts) == 0 {
		logger.Infof("DRY RUN: no create/update/delete operations were planned")
		return
	}
	methods := sortedKeys(counts)
	parts := make([]string, 0, len(methods))
	total := 0
	for _, method := range methods {
		parts = append(parts, fmt.Sprintf("%s:%d", method, counts[method]))
		total += counts[method]
	}
	logger.Infof("DRY RUN: skipped %d mutation calls (%s)", total, strings.Join(parts, ", "))
}
