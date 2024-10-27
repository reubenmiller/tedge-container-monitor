set positional-arguments
set dotenv-load
set export

IMAGE := env_var_or_default("IMAGE", "debian-systemd")
IMAGE_SRC := env_var_or_default("IMAGE_SRC", "debian-systemd")

# Release all artifacts
release *ARGS='':
    mkdir -p output
    go run main.go completion bash > output/completions.bash
    go run main.go completion zsh > output/completions.zsh
    go run main.go completion fish > output/completions.fish

    docker context use default
    goreleaser release --clean --auto-snapshot {{ARGS}}

# Build a release locally (for testing the release artifacts)
release-local:
    just -f "{{justfile()}}" release --snapshot

# Install python virtual environment
venv:
  [ -d .venv ] || python3 -m venv .venv
  ./.venv/bin/pip3 install -r tests/requirements.txt

# Build test images
build-test:
  docker build -t {{IMAGE}} -f ./test-images/{{IMAGE_SRC}}/Dockerfile .

# Run tests
test *args='':
  ./.venv/bin/python3 -m robot.run --outputdir output {{args}} tests
