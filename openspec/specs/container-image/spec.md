# container-image Specification

## Purpose
Defines the OCI image that packages `tsgit` for deployment: what it contains,
how it runs, and which persistent paths and privileges an operator can rely on.

## Requirements

### Requirement: The project builds a runnable container image

The project SHALL provide a container definition that builds `tsgit` from the
repository source and produces an OCI image runnable on `linux/amd64` and
`linux/arm64` without additional build steps on the operator's machine.

#### Scenario: Image builds from a clean checkout
- **WHEN** an operator runs a container build against the repository root
- **THEN** the build completes successfully and produces an image tagged with an
  operator-chosen name

#### Scenario: Image reports the injected version
- **WHEN** the built image is run with the `version` argument
- **THEN** it prints the version supplied at build time and exits successfully
- **AND** it does not print the development placeholder `dev` for a release
  build

### Requirement: The image contains the runtime dependencies of `tsgit serve`

The image SHALL contain the `tsgit` binary, a `git` executable usable by the
serving backend, and CA certificates sufficient to reach the Tailscale control
plane over HTTPS.

#### Scenario: Git backend is available inside the image
- **WHEN** a command is run in the image to report the git version
- **THEN** the command succeeds and reports a git version supported by the
  project

#### Scenario: Git operations succeed without host packages
- **WHEN** the container is started with a writable repository root and a
  request causes repository creation
- **THEN** repository initialization succeeds using only binaries and
  certificates present in the image

### Requirement: The image runs as an unprivileged user with a stable identity

The image SHALL run `tsgit` as a fixed non-root numeric user and group, and the
state directory and repository root SHALL be writable by that user.

#### Scenario: Default process is not root
- **WHEN** the image's default command is inspected or a shell command is run in
  the image
- **THEN** the effective user id is non-zero and matches the documented image
  user

#### Scenario: Volumes are writable by the image user
- **WHEN** the container is started with fresh volumes for state and
  repositories
- **THEN** `tsgit serve` starts without a permission error and can create its
  state and repository directories

#### Scenario: Wrong volume ownership fails clearly
- **WHEN** a mounted repository root is not writable by the image user
- **THEN** startup fails with an error that names the path, rather than
  continuing with a partially working service

### Requirement: Persistent data uses separate declared paths

The image SHALL keep tsnet state, including the node key, separate from the
repository root and SHALL expose both as declared volumes at documented paths.
The container SHALL be restartable without losing node registration or
repositories when those volumes are reused.

#### Scenario: State and repositories survive container replacement
- **WHEN** a container is removed and a new container is started with the same
  state and repository volumes
- **THEN** the node reuses its existing registration and the repositories remain
  cloneable

#### Scenario: Volumes are separate
- **WHEN** an operator inspects the image configuration
- **THEN** the state path and the repository root are distinct paths and both
  are declared as volumes

### Requirement: The image starts `tsgit serve` with container-appropriate defaults

The image SHALL default to running `tsgit serve` and SHALL point `--state-dir`
and `--repo-root` at the declared volume paths. Configuration SHALL remain
available through the existing command-line flags and environment variables.

#### Scenario: No arguments starts the server
- **WHEN** the image is run without command arguments and the required volumes
  are mounted
- **THEN** the process runs `tsgit serve` and listens according to its
  configuration

#### Scenario: Flags and environment override defaults
- **WHEN** the container is run with a documented `TSGIT_*` environment variable
  or an explicit `serve` flag
- **THEN** the supplied value overrides the image default without requiring a
  different image

#### Scenario: Development mode is usable for inspection
- **WHEN** the image is run with `serve --dev-listen 0.0.0.0:8080` and the port
  is published
- **THEN** an HTTP git client can reach the development server without a
  tailnet

### Requirement: The image contains no credentials or tailnet state

The image SHALL NOT bake in a Tailscale auth key, node state, repository
content, or other secret. Authentication material SHALL be supplied at runtime
through the existing environment variable or auth key file mechanism.

#### Scenario: Image configuration has no auth key
- **WHEN** the built image's environment and filesystem are inspected
- **THEN** no Tailscale auth key or `tailscaled.state` file is present

#### Scenario: Runtime auth key is honored
- **WHEN** the container is started with `TS_AUTHKEY` or a mounted auth key file
- **THEN** the node joins the tailnet using that runtime-supplied credential
