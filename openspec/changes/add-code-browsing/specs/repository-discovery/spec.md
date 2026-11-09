# Spec Delta

## MODIFIED Requirements

### Requirement: Repository summary

For a repository the viewer may read, the system MUST render a summary
containing the description, default branch, latest commit, clone URL, and, when
the default branch contains a README, that README rendered by the
`code-browsing` renderer. When the repository is empty, the summary MUST show
how to push a first commit, except for a Funnel request, which MUST NOT show
first-push commands. When the viewer has `admin`, the summary MUST offer a
deletion action that links to a separate confirmation surface; the summary
itself MUST NOT delete anything. The summary MUST NOT render file trees,
non-README file contents, diffs, or commit lists.

#### Scenario: Populated repository

- **WHEN** the viewer opens a non-empty repository they may read
- **THEN** the summary shows its description, default branch, latest commit,
  and clone URL

#### Scenario: Empty repository

- **WHEN** the viewer opens a repository whose default branch has no commits
- **THEN** the summary identifies it as empty and shows the commands to push a
  first commit

#### Scenario: Default branch is unborn but other refs exist

- **WHEN** the repository's `HEAD` names a branch with no commits while another
  branch exists
- **THEN** the summary does not present the repository as never-pushed, and it
  names the existing refs

#### Scenario: Metadata is unavailable

- **WHEN** one repository's metadata cannot be read
- **THEN** the index still renders that repository and the other repositories,
  and the failure is logged

#### Scenario: Public empty repository hides push instructions

- **WHEN** a Funnel request opens an empty public repository
- **THEN** the summary identifies it as empty but shows no first-push commands

#### Scenario: Admin sees a deletion action

- **WHEN** the viewer has `admin` on a repository they may read
- **THEN** the summary offers a deletion action that leads to a separate
  confirmation surface, and the summary request itself changes nothing

#### Scenario: Writer does not see a deletion action

- **WHEN** the viewer has `write` but not `admin` on a repository they may read
- **THEN** the summary shows the repository as writable and offers no deletion
  action

#### Scenario: README appears on the summary

- **WHEN** the default branch of a readable repository contains a README
- **THEN** the summary displays the README rendered through the `code-browsing`
  renderer, below the repository metadata

#### Scenario: Missing README leaves the summary intact

- **WHEN** the default branch contains no recognized README
- **THEN** the summary renders its metadata normally and shows no README section

#### Scenario: README failure does not fail the summary

- **WHEN** the README cannot be read or rendered
- **THEN** the summary still renders the repository metadata, reports that the
  README is unavailable, and logs the failure

#### Scenario: Summary does not render other files

- **WHEN** the default branch contains files other than the README
- **THEN** the summary does not render their contents, a file tree, a diff, or a
  commit list

#### Scenario: Public summary renders the README safely

- **WHEN** a Funnel request opens the summary of a public repository whose
  default branch contains a README
- **THEN** the README is rendered under the same safe-mode and link rules used on
  the tailnet, and the page shows no push or delete affordance
