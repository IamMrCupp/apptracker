# Releasing

Releases are automated by [release-please](https://github.com/googleapis/release-please).
Merging to `main` updates a standing release PR; merging *that* cuts the release.

Two steps need a specific command rather than the obvious one. Both are
consequences of the same GitHub behaviour: **anything created with the built-in
`GITHUB_TOKEN` does not trigger workflow events**, to prevent recursive runs.

## Merging a normal PR

```sh
gh pr merge <N> --merge --body ""
```

The empty body matters. A GitHub merge commit is titled
`Merge pull request #N from …`, which release-please cannot parse — but it then
falls back to scanning the commit *body* for a conventional commit, which exists
to support squash-and-merge workflows. This repo's merge-commit body defaults to
the PR title, which **is** a conventional commit, so the change gets counted
twice and appears twice in the changelog.

`--body ""` leaves nothing to fall back to. The merge commit is skipped and only
the feature commit is counted.

GitHub offers no repo setting for this: the only allowed title/body combinations
are `PR_TITLE`+`PR_BODY`, `PR_TITLE`+`BLANK`, and `MERGE_MESSAGE`+`PR_TITLE`, and
the first two are worse — they put the conventional message in the merge commit
title, where it parses directly. See issue #40.

## Merging the release PR

```sh
gh pr merge <N> --merge --admin
```

`main` is protected by a ruleset requiring four status checks. Release PRs carry
**zero** checks, because `GITHUB_TOKEN`-created PRs do not trigger workflows — so
without `--admin` the release PR can never merge.

The ruleset grants repository admins a bypass, but it has to be invoked
explicitly; it does not apply silently. Protection still holds for everything
else, and direct pushes to `main` are still rejected.

The proper fix is to have release-please authenticate with a PAT so its PRs get
real checks. That trades a stored credential for a real gate on generated
content. See issue #41.

## What happens on a release

Merging the release PR runs `release-please.yml`, which:

1. Tags `apptracker-vX.Y.Z` — note the package-name prefix; it is not a bare `vX.Y.Z`
2. Creates the GitHub Release and updates `CHANGELOG.md`
3. Bumps `newTag` in `deploy/base/kustomization.yaml` via the
   `# x-release-please-version` annotation
4. Calls `publish.yml` to push `X.Y.Z`, `X.Y` and `latest` to
   `ghcr.io/iammrcupp/apptracker`, for `linux/amd64` and `linux/arm64`

`publish.yml` is invoked by `workflow_call`, **not** a `release: published`
trigger — that trigger would never fire, for the same `GITHUB_TOKEN` reason.

## Versioning

Pre-1.0 by choice. `bump-minor-pre-major: true` keeps features on minor bumps
(`0.2.0` → `0.3.0`) rather than jumping to `1.0.0`. Planned desktop work changes
the default `DB_PATH`, which is breaking for adopters on defaults, and staying
`0.x` keeps that cheap.

## Regenerating a release PR

release-please does not retrofit an already-open release PR when config changes.
Close it, then:

```sh
gh workflow run release-please.yml --ref main
```
