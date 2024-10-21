# tedge-container-monitor

This is a temporary repository which is used for development of a refactored tedge-container-plugin (though just the tedge-container-monitor part).

It will be moved to the [tedge-container-plugin](https://github.com/thin-edge/tedge-container-plugin) once it is proven to be a valuable replacement for the current posix shell implementation.

# TODO

## Phase 1

* [x] Register container and container-groups to thin-edge.io
* [x] Publish container meta information via `/twin/container` topic
* [x] Delete orphaned services from the cloud

* [ ] Subscribe to `te/device/main/service/+/cmd/health/check` to support on demand triggering to refresh container state

* [ ] Support using certificates to interact with:
    * [ ] MQTT broker
    * [ ] Cumulocity Local Proxy

* [ ] Read config from file and environment variables

* [ ] Support filter criteria to only pick specific containers with the given labels

* [ ] Publish telemetry data (in same format at docker stats)

* [ ] Support filtering on container name

* [ ] One scan option - Don't register a service, and let users trigger it via systemd timer

* [ ] Periodically poll mode

* [ ] Configuration
    * [ ] Enable/disable telemetry data
    * [ ] Enable/disbale meta info
    * [ ] Enable/disbale compose project monitoring
    * [ ] Enable/disbale container monitoring

* [ ] Build workflow
    * [ ] Linux packages
    * [ ] Container image


## Phase 2

* [ ] Support fetching container logs

* [ ] Support executing custom command in container?

