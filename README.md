# tedge-container-plugin

This is a temporary repository which is used for development of a refactored tedge-container-plugin.

It will be moved to the [tedge-container-plugin](https://github.com/thin-edge/tedge-container-plugin) once it is proven to be a valuable replacement for the current posix shell implementation.

## TODO

### Phase 1

* [x] Register container and container-groups to thin-edge.io
* [x] Publish container meta information via `/twin/container` topic
* [x] Delete orphaned services from the cloud
* [x] One scan option - Don't register a service, and let users trigger it via systemd timer
* [x] Periodically poll mode
* [x] Build workflow
    * [x] Linux packages
    * [x] Container image

* [x] Support filtering on container name

* [x] Subscribe to `te/device/main/service/+/cmd/health/check` to support on demand triggering to refresh container state

* [x] Support filter criteria to only pick specific containers with the given labels

* [x] Configuration
    * [x] Enable/disable telemetry data
    * [x] Enable/disable compose project monitoring
    * [x] Enable/disable container monitoring

* [x] Support excluding containers with a give label

* [x] Support excluding containers by name

* [x] Add subcommand for
    * [x] container sm-plugin

* [x] Publish telemetry data (in same format at docker stats)

* [x] Read config from file and environment variables

* [x] Support using certificates to interact with:
    * [x] MQTT broker
    * [x] Cumulocity Local Proxy

* [ ] Fix bug where fetching metrics affects the subscription to the container engine events (or the handling of incoming events)

### Phase 2

* [ ] Fix container id to container-group service lookup (triggered from the system events)

* [ ] Support fetching container logs

* [ ] Support start/stop/restart/pause/unpause container

* [ ] Support executing custom command in container?
