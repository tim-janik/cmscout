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
Without a tag or a baked `.version`, make stops with an error.

To release, replace `Unreleased` in NEWS.md with the release version and commit it.
For example, use `## Cmscout 0.1.0`, then create and push an annotated tag:

```sh
git tag -a v0.1.0 -m 'cmscout 0.1.0'
git push origin v0.1.0
```

CI builds and checks the archives, then creates a draft with the matching NEWS entry.
Review its notes and downloads on GitHub, then publish it.

For a nightly release, push a lightweight tag named after the last release:

```sh
tag=v0.1.0.nightly$(date -u +%y%m%d%H%M)
git tag "$tag" && git push origin "$tag"
```

CI then creates a public prerelease with notes from Git history.
Only the tag type selects draft or prerelease, tag suffixes do not.
`gh-release.sh` refuses a tag that differs from the version in `.version`,
for example a nightly tag on a commit that also has an annotated tag.

For a local preview, set `DOCKER_IMAGE` and `DOCKER_ENV` to the values in ci.yml,
check out the tagged commit, and run:

```sh
.github/workflows/gh-release.sh --docker v0.1.0
```

This builds and checks the archives, then prints the release command.
Add `--upload` to create the GitHub release using your `gh` credentials.
Omit `--docker` to use local build tools.

## Daily distcheck

[distcheck.yml](https://github.com/tim-janik/cmscout/blob/trunk/.github/workflows/distcheck.yml) runs `make distcheck` daily when
the latest commit is less than 25 hours old. A manual run always builds.
It uses the release Go image, keeps artifacts for seven days, and reports the result
on IRC. It does not create tags or GitHub releases.

## Shared scripts

`gh-release.sh`, `release-test.sh`, `ircbot.py`, and `test_ircbot.py` are copied unchanged from
[jj-fzf at c6f40e9](https://github.com/tim-janik/jj-fzf/tree/c6f40e93097b76876784b3f4c0f5ffe8c05fbf92/.github/workflows),
and so are `.version` and `.gitattributes`. The Makefile reads `.version` the same way.
Keep these copies in sync; project-specific build and packaging commands belong
in the Makefile and workflows.
