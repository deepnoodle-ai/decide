# Releasing

A release is a `v*` tag on `main`. The [release workflow](../.github/workflows/release.yml)
builds and publishes everything else. Release only when a maintainer says
to.

## What a release publishes

- Binaries for Linux, macOS, and Windows, on amd64 and arm64.
  `decide --version` prints the tag.
- A GitHub release with the archives, `checksums.txt`, and notes made from
  the titles of the merged pull requests.
- `Formula/decide.rb` in
  [deepnoodle-ai/homebrew-tap](https://github.com/deepnoodle-ai/homebrew-tap),
  for `brew install deepnoodle-ai/tap/decide`.

Archive names have no version, such as `decide_linux_amd64.tar.gz`, so the
[recipes](recipes.md) can download `releases/latest`. Keep the names
unchanged, or those recipes break.

## Choose the version

Before v1, any release may change the library API and the CLI.

- Increase the minor version, such as `v0.2.0`, for new features or
  changes that break something.
- Increase the patch version, such as `v0.1.1`, for fixes only.
- Add a suffix, such as `v0.2.0-rc.1`, for a prerelease. GitHub marks it
  as a prerelease, `releases/latest` skips it, and the Homebrew formula
  stays on the last full release.

## Release

1. Make sure CI passes on `main`. CI runs `goreleaser check`, so a broken
   release configuration fails there first.
2. Tag the commit and push the tag:

   ```sh
   git switch main && git pull
   git tag v0.2.0
   git push origin v0.2.0
   ```

3. Watch the run with `gh run watch`. It runs the tests, then GoReleaser.
4. Check the result:

   ```sh
   brew upgrade decide && decide --version
   ```

## When a release fails

Do not move or reuse a tag that people may have downloaded. Fix the
problem on `main` with a pull request, then tag the next patch version.

If the workflow failed before it published anything, delete the tag and
push it again on the fixed commit:

```sh
git push origin :refs/tags/v0.2.0 && git tag -d v0.2.0
```

## Test a release locally

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish
./dist/decide_darwin_arm64*/decide --version
```

The snapshot goes in `dist/`, which git ignores.

## Settings

- The `TAP_GITHUB_TOKEN` secret lets the workflow write to the Homebrew
  tap. It needs write access to `contents` in that repository only.
- GoReleaser is pinned to v2.15.3 in the release workflow and in CI. The
  `brews` section is deprecated, so upgrade on purpose and run
  `goreleaser check` when you do.
- Homebrew and the download links in the recipes work only while this
  repository is public.
