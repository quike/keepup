package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"maps"

	"github.com/quike/keepup/core/config"
)

type Logger interface {
	Info() LogEvent
	Error() LogEvent
	Trace() LogEvent
	Warn() LogEvent
}

type LogEvent interface {
	Msgf(format string, v ...interface{})
}

type Executor struct {
	groups  *sync.Map
	Config  config.Config
	Outputs sync.Map
	log     Logger
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewExecutor(cfg config.Config, log Logger) *Executor {
	groupMap := &sync.Map{}
	for _, g := range cfg.Groups {
		groupMap.Store(g.Name, g)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Executor{
		groups:  groupMap,
		Config:  cfg,
		Outputs: sync.Map{},
		log:     log,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (e *Executor) Cancel() {
	e.cancel()
}

func (e *Executor) Run() error {
	for stepIndex, step := range e.Config.Execution {
		select {
		case <-e.ctx.Done():
			return e.ctx.Err()
		default:
		}

		e.log.Info().Msgf("Starting step %d: %v", stepIndex+1, step.Group)

		var wg sync.WaitGroup
		errs := make(chan error, len(step.Group))

		sem := make(chan struct{}, e.semaphoreSize(step))

		for _, groupName := range step.Group {
			group, ok := e.groups.Load(groupName)
			if !ok {
				return fmt.Errorf("group %s not defined", groupName)
			}

			sem <- struct{}{}
			wg.Add(1)
			go func(g config.Group) {
				defer func() {
					<-sem
					wg.Done()
				}()
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
				e.log.Error().Msgf("Error: %s", err)
			}
			return fmt.Errorf("step %d failed", stepIndex+1)
		}
		e.log.Info().Msgf("Step %d completed successfully", stepIndex+1)
	}

	return nil
}

func (e *Executor) semaphoreSize(step config.Step) int {
	max := 0
	for _, name := range step.Group {
		if g, ok := e.groups.Load(name); ok {
			group := g.(config.Group)
			if group.Concurrency != nil {
				switch v := group.Concurrency.(type) {
				case int:
					if v > max {
						max = v
					}
				}
			}
		}
	}
	if max > 0 {
		return max
	}
	return len(step.Group)
}

func (e *Executor) runGroup(group config.Group) error {
	shell := group.Shell
	if shell == "" {
		shell = getDefaultShell()
	}

	expandedParams := make([]string, len(group.Params))
	for i, param := range group.Params {
		expandedParams[i] = e.expandParams(param)
	}

	fullCmd := fmt.Sprintf("%s %s", group.Command, strings.Join(expandedParams, " "))

	cmd := exec.CommandContext(e.ctx, shell, "-c", fullCmd)

	cmd.Env = mergeEnvs(
		os.Environ(),
		e.Config.Env,
		group.Env,
	)

	if e.Config.Settings.WorkingDir != "" {
		cmd.Dir = e.Config.Settings.WorkingDir
	}

	var output bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)

	e.log.Info().Msgf("Running group %s: command %s %v", group.Name, group.Command, expandedParams)

	if err := cmd.Run(); err != nil {
		e.log.Error().Msgf("Error running %s: %s", group.Name, err)
		e.log.Info().Msgf("Output from %s: %s", group.Name, output.String())
		return err
	}

	e.log.Trace().Msgf("Output from %s: %s", group.Name, output.String())
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
		shell = "/bin/sh"
	}
	return shell
}

func mergeEnvs(base []string, overrides ...map[string]string) []string {
	result := map[string]string{}

	for _, raw := range base {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}

	for _, layer := range overrides {
		maps.Copy(result, layer)
	}

	final := make([]string, 0, len(result))
	for k, v := range result {
		final = append(final, fmt.Sprintf("%s=%s", k, v))
	}
	return final
}
