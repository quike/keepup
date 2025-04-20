package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"maps"

	"github.com/quike/keepup/core/config"
	"github.com/quike/keepup/logger"
)

type Executor struct {
	// mutex   sync.Mutex
	Groups  *sync.Map
	Config  config.Config
	Outputs sync.Map // program name -> output
}

func NewExecutor(cfg config.Config) *Executor {
	groupMap := &sync.Map{}
	for _, g := range cfg.Groups {
		groupMap.Store(g.Name, g)
	}
	return &Executor{
		Groups:  groupMap,
		Config:  cfg,
		Outputs: sync.Map{},
	}
}

func (e *Executor) Run() error {
	for stepIndex, step := range e.Config.Execution {
		logger.GetLogger().Info().Msgf("Starting step %d: %v", stepIndex+1, step.Group)

		var wg sync.WaitGroup
		errs := make(chan error, len(step.Group))

		for _, groupName := range step.Group {
			group, ok := e.Groups.Load(groupName)
			if !ok {
				return fmt.Errorf("group %s not defined", groupName)
			}

			wg.Add(1)
			go func(g config.Group) {
				defer wg.Done()
				err := e.runGroup(g)
				if err != nil {
					errs <- fmt.Errorf("group %s failed: %w", g.Name, err)
				}
			}(group.(config.Group))
		}

		wg.Wait()
		close(errs)

		if len(errs) > 0 {
			for err := range errs {
				logger.GetLogger().Error().Msgf("Error: %s", err)
			}
			return fmt.Errorf("step %d failed", stepIndex+1)
		}
		logger.GetLogger().Info().Msgf("Step %d completed successfully", stepIndex+1)
	}

	return nil
}

func (e *Executor) runGroup(group config.Group) error {

	// e.mutex.Lock()
	// defer e.mutex.Unlock()

	shell := group.Shell
	if shell == "" {
		shell = getDefaultShell()
	}

	// Expand parameters
	expandedParams := make([]string, len(group.Params))
	for i, param := range group.Params {
		expandedParams[i] = e.expandParams(param)
	}

	// Build final command line string
	fullCmd := fmt.Sprintf("%s %s", group.Command, strings.Join(expandedParams, " "))

	cmd := exec.Command(shell, "-c", fullCmd)

	cmd.Env = mergeEnvs(
		os.Environ(), // base env
		e.Config.Env, // global env (e.Config.Env)
		group.Env,    // group-specific env (this is g.Env)
	)

	var output bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)

	logger.GetLogger().Info().Msgf("Running group %s: command %s %v", group.Name, group.Command, expandedParams)

	if err := cmd.Run(); err != nil {
		logger.GetLogger().Error().Msgf("Error running %s: %s", group.Name, err)
		logger.GetLogger().Info().Msgf("Output from %s: %s", group.Name, output.String())
		return err
	}

	logger.GetLogger().Trace().Msgf("Output from %s: %s", group.Name, output.String())
	e.Outputs.Store(group.Name, output.String())

	return nil
}

func (e *Executor) expandParams(param string) string {
	e.Outputs.Range(func(key, value interface{}) bool {
		placeholder := fmt.Sprintf("{{ output.%s }}", key.(string))
		param = strings.ReplaceAll(param, placeholder, strings.TrimSpace(value.(string)))
		return true
	})
	return param
}

func getDefaultShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh" // Fallback to /bin/sh if SHELL env var is not set
	}
	return shell
}

func mergeEnvs(base []string, overrides ...map[string]string) []string {
	result := map[string]string{}

	// Convert base []string to map
	for _, raw := range base {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}

	// Apply overrides in order
	for _, layer := range overrides {
		maps.Copy(result, layer)
	}

	// Convert back to []string
	final := make([]string, 0, len(result))
	for k, v := range result {
		final = append(final, fmt.Sprintf("%s=%s", k, v))
	}
	return final
}
