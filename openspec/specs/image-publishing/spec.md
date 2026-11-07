# image-publishing Specification

## Purpose
Defines how CI builds, verifies, and publishes the `tsgit` container image to
GitHub Container Registry, including the triggers, credentials, and tags an
operator can rely on.

## Requirements

### Requirement: Pull requests build and smoke-test the image without publishing

CI SHALL build the container image for every pull request and SHALL verify that
the resulting image runs by invoking its `version` command. A pull request run
SHALL NOT push to any registry and SHALL NOT require registry credentials.

#### Scenario: Pull request builds the image
- **WHEN** a pull request is opened or updated
- **THEN** CI builds the image from the pull request merge commit

#### Scenario: Pull request smoke test runs the image
- **WHEN** the pull request image build succeeds
- **THEN** CI runs the image's `version` command and the workflow fails if the
  command fails

#### Scenario: Fork pull request cannot publish
- **WHEN** a pull request from a fork runs the image workflow
- **THEN** no registry credentials are used and no image is pushed

### Requirement: Main and release builds publish to GitHub Container Registry

CI SHALL publish the image to GitHub Container Registry under the repository's
owner and name, using only the workflow's built-in `GITHUB_TOKEN`. A push to
`main` and a push of a semver tag SHALL publish; pull requests and any other
ref SHALL NOT.

#### Scenario: Push to main publishes moving and commit tags
- **WHEN** a commit is pushed to `main`
- **THEN** CI publishes the image tagged `main` and a tag derived from the commit
  SHA

#### Scenario: Semver tag publishes release tags
- **WHEN** a tag matching `v<major>.<minor>.<patch>` is pushed
- **THEN** CI publishes the image tagged with the exact tag and `latest`

#### Scenario: Other refs do not publish
- **WHEN** the image workflow runs for a pull request or any ref other than
  `main` or a semver tag
- **THEN** CI builds and smoke-tests the image but does not publish it

#### Scenario: Registry credentials are scoped
- **WHEN** the publishing job runs
- **THEN** it has repository read access and package write access only, and it
  does not use a long-lived personal access token

### Requirement: Publication is gated on the project test contract

CI SHALL NOT publish an image for a commit that fails the project's
formatting, lint, build, and test checks. A failed build or smoke test SHALL
fail the workflow before any image is pushed.

#### Scenario: Failing tests block publication
- **WHEN** `just ci` fails for a commit on `main`
- **THEN** the image workflow fails and no image tag is pushed for that commit

#### Scenario: Failed smoke test blocks publication
- **WHEN** the image builds but the `version` smoke test fails
- **THEN** the workflow fails before pushing any tag

### Requirement: Published images are multi-architecture manifests

A published image SHALL be a single manifest that runs on both `linux/amd64`
and `linux/arm64`, so an operator can pull the same tag on either architecture.

#### Scenario: Pull by architecture
- **WHEN** an operator pulls a published tag on an amd64 host
- **THEN** the registry serves the amd64 image from the manifest

#### Scenario: Same tag on arm64
- **WHEN** an operator pulls the same published tag on an arm64 host
- **THEN** the registry serves the arm64 image from the manifest

### Requirement: The image workflow is independent of the test workflow

Image work SHALL run in its own workflow separate from the existing `just ci`
workflow, so a registry or image-build failure does not replace or mask the
project's formatting, lint, build, and test result.

#### Scenario: Registry outage does not hide test status
- **WHEN** the registry is unreachable or image publishing fails
- **THEN** the `just ci` workflow still reports its own independent result

#### Scenario: Image workflow reports its own failure
- **WHEN** the image build or publish step fails
- **THEN** the image workflow run is marked failed with the failing step
  identified
