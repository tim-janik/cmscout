<!-- This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0 -->

# Releasing

`make dist` creates the source archive from the current commit.
`make distcheck` extracts it, builds and tests cmscout, and packages that build.
The binary archive includes the Git wrapper, docs, and license texts.
Both archives and `cmscout-VERSION.SHA256SUMS` end up in `artifacts/`.

Local builds need Go 1.25+, a C compiler, Git, Make, and xz.
The release workflow uses the Go image and environment in
[ci.yml](https://github.com/tim-janik/cmscout/blob/trunk/.github/workflows/ci.yml).

## Version and tags

The version comes from Git tags, without the leading `v`.
`git archive` writes the tag and commit date into `.version` through
`export-subst`. Builds from the source archive read that file without Git metadata.
`make version` prints the version and date; `cmscout --version` prints the version.

To release, replace `Unreleased` in NEWS.md with the release version and commit it.
For example, use `## Cmscout 0.1.0`, then create and push an annotated tag:

```sh
git tag -a v0.1.0 -m 'cmscout 0.1.0'
git push origin v0.1.0
```

CI builds and checks the archives, then creates a draft with the matching NEWS entry.
Review its notes and downloads on GitHub, then publish it.
A lightweight tag creates a public prerelease with notes from Git history instead.
Tag suffixes do not change this choice.

For a local preview, set `DOCKER_IMAGE` and `DOCKER_ENV` to the values in ci.yml,
check out the tagged commit, and run:

```sh
.github/workflows/gh-release.sh --docker v0.1.0
```

This builds and checks the archives, then prints the release command.
Add `--upload` to create the GitHub release using your `gh` credentials.
Omit `--docker` to use local build tools.

## Nightly checks

[nightly.yml](https://github.com/tim-janik/cmscout/blob/trunk/.github/workflows/nightly.yml) runs `make distcheck` daily when
the latest commit is less than 25 hours old. A manual run always builds.
It uses the release Go image, keeps artifacts for seven days, and reports the result
on IRC. It does not create tags or GitHub releases.

## Shared scripts

`gh-release.sh`, `ircbot.py`, and `test_ircbot.py` are copied unchanged from
[Anklang at a678a918](https://github.com/tim-janik/anklang/tree/a678a9185d0b86ce9f0cdf401f78d2d25108bbc1/.github/workflows).
The `.version` file and its Makefile rules use the same mechanism.
Keep these copies in sync; project-specific build and packaging commands belong
in the Makefile and workflows.
