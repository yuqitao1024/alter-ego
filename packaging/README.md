# Packaging

This directory contains the generic packaging flow for deploying Alter Ego onto a Linux host with `systemd`.

## Build

Create a release archive with the example configuration and an empty environment template:

```sh
./packaging/build-package.sh
```

The script cross-compiles:

- `GOOS=linux`
- `GOARCH=amd64`
- `CGO_ENABLED=0`

Override if needed:

```sh
GOARCH=arm64 VERSION=test ./packaging/build-package.sh
```

## Archive Layout

The generated `tar.gz` contains a root-style filesystem tree:

```text
alterego/
  opt/alterego/bin/alterego
  opt/alterego/config/configs/machines/example.yaml
  opt/alterego/config/configs/repositories/example.yaml
  opt/alterego/config/configs/templates/example.yaml
  opt/alterego/config/docs/workflows/example-feature-dev.md
  etc/alterego/alterego.env.example
  etc/systemd/system/alteregod.service
  var/lib/alterego/
```

The service expects:

- binary: `/opt/alterego/bin/alterego`
- config root: `/opt/alterego/config`
- environment file: `/etc/alterego/alterego.env`
- SQLite state: `/var/lib/alterego/tasks.db`

Remote task execution is configured from the unpacked repository tree under `/opt/alterego/config`; machine YAML now carries the app-server socket and bootstrap command list for each remote host.
Remote task execution is configured from the unpacked repository tree under `/opt/alterego/config`; machine YAML must include the Codex app-server fields:

- `app_server_listen_host`
- `app_server_listen_port`
- `app_server_service_name`
- `app_server_install_user`

The packaged example configs already include these fields.

## Notes

- The committed packaging flow never includes real secrets or real deployment configuration.
- Use the example configs as a safe starting point.
- Real environment packaging and one-click deployment scripts should stay in ignored local files under `packaging/local/`.
