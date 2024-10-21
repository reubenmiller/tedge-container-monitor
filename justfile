
# Build for current target
build *ARGS='':
    goreleaser build --clean --snapshot --single-target {{ARGS}}

# Release all artifacts
release *ARGS='':
    mkdir -p output
    go run main.go completion bash > output/completions.bash
    go run main.go completion zsh > output/completions.zsh
    go run main.go completion fish > output/completions.fish

    docker context use default
    goreleaser release --clean --auto-snapshot {{ARGS}}

release-local:
    just -f "{{justfile()}}" release --snapshot
