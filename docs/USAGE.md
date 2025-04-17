# Usage

## Information

This is a simple task runner built in go.

## Configure

By default the program tries to find the `keepup.yml` file within `$HOME/.config/keepup` directory.

Example (this content may change):

```yaml
---
version: 1

settings:
  logging:
    level: debug
    pretty: true # set to true for human-readable logs, false for json

groups:
  - name: brew-update
    description: "Execute brew update"
    command: "brew"
    params: ["update"]

  - name: brew-upgrade
    description: "Execute brew upgrade"
    command: "brew"
    params: ["upgrade"]

  - name: brew-cleanup
    description: "Execute brew cleanup"
    command: "brew"
    params: ["cleanup"]

  - name: omf-update
    description: "Update Oh My Fish"
    command: "omf"
    params: ["update"]
    shell: /opt/homebrew/bin/fish

  - name: omf-reload
    description: "Reload Oh My Fish"
    command: "omf"
    params: ["reload"]
    shell: /opt/homebrew/bin/fish

  - name: fisher-update
    description: "Update Fisher"
    command: "fisher"
    params: ["update"]
    shell: /opt/homebrew/bin/fish

execution:
  - group: ["brew-update"]
  - group: ["brew-upgrade"] # runs after previous group execution
  - group: ["brew-cleanup"]
  - group: ["omf-update", "fisher-update"] # these run concurrently
  - group: ["omf-reload"]
```

Users can also pass a defined config with `-c <file>` or `--config <file>` flags.

### Environment Variables

Program accepts global and scoped env substitutions

```yml
env:
  WHATEVER: "global value" # <-- this is a global environment variable

groups:
  - name: global-env
    description: "This runs program 1"
    command: "echo $WHATEVER" # <-- It will use the global definition
    params: ["--flag1", "value1"]

  - name: scoped-env
    description: "Runs program 2 with something"
    command: "echo $WHATEVER" # <-- It will use the scoped definition
    params: ["--config", "/etc/prog2.yaml"]
    env:
      WHATEVER: "scoped value" # <-- this is a scoped environment variable >
```

### Shells

Program accepts to run commands under specific shells. If ommited `/bin/sh` will be used by default.

```yml
groups:
  - name: fisher-update
    description: "Update Fisher"
    command: "fisher"
    params: ["update"]
    shell: /opt/homebrew/bin/fish # <-- fish shell will be used to execute command
```

## Commands

## Root

The root command will execute the task groups.

### Version

Print the version information.

```yaml
❯ keepup version
{"arch":"arm64","os":"darwin","sha":"41b2a38","version":"0.0.0"}
```
