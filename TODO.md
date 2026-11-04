# TODO

## Distribution

- [ ] **Publish the AUR package on release.** Today `packaging/aur/PKGBUILD` is pushed to the AUR by hand. Add a workflow that runs when a GitHub release is *published* (releases start as drafts, so not on tag push): set `pkgver`, build the package to check it, regenerate `.SRCINFO` and push to the AUR repo with an SSH key stored as a secret. t3code does this in `.github/workflows/publish-aur.yml` and `packaging/aur/scripts/release.sh` (pingdotgg/t3code).
- [ ] **List the app in other package managers.** These live in each platform's own repo, not ours, and their bots usually pick up new GitHub releases:
  - Homebrew cask (`brew install --cask`): check whether an unsigned macOS app is accepted
  - winget: a manifest in `microsoft/winget-pkgs`
  - Scoop: the community `extras` bucket
