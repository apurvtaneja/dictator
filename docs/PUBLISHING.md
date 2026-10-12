# Publishing Dictator

Dictator now uses only the official OpenRouter API with each user's own key, so nothing
stands in the way of a public release. The steps below are in rough order.

## 1. Before the first public release

- [ ] Add a `LICENSE` file (MIT is the usual choice for a small utility) and set
      `NSHumanReadableCopyright` in `packaging/Info.plist` to match.
- [ ] Add a `CHANGELOG.md`.
- [ ] Tag a version: `git tag v1.0.0`. `make` stamps the tag into the app.
- [ ] Smoke test on a fresh macOS user account: install, add a key, allow the microphone,
      dictate into TextEdit, then try it without Accessibility (clipboard path).

## 2. Signing and notarization (strongly recommended)

`make` signs with the local "Dictator Local Signing" identity from `make cert`, or ad hoc
without it. Either is fine on your own Mac, but Gatekeeper trusts neither on other Macs: a
downloaded copy shows *"Apple could not verify Dictator is free of malware"*, and the user has to
click **Open Anyway** in System Settings ▸ Privacy & Security once. The local identity at least
keeps the permission requirement stable, so people who allowed Accessibility for one release
keep it for the next, as long as every release is signed with the same certificate. Back up
that certificate (Keychain Access ▸ export as .p12); a new one resets everyone's permissions.

1. Join the Apple Developer Program ($99/year) and create a **Developer ID Application**
   certificate.
2. Sign with the hardened runtime and the existing entitlements (the microphone entitlement
   is the only one needed):
   ```sh
   codesign --force --options runtime --timestamp \
     --entitlements packaging/entitlements.plist \
     --sign "Developer ID Application: <Name> (<TEAMID>)" dist/Dictator.app
   ```
3. Package the DMG and notarize it:
   ```sh
   hdiutil create -volname Dictator -srcfolder dist/Dictator.app -ov -format UDZO dist/Dictator-1.0.0.dmg
   xcrun notarytool submit dist/Dictator-1.0.0.dmg --keychain-profile notary --wait
   xcrun stapler staple dist/Dictator-1.0.0.dmg
   ```

The app is universal (arm64 + x86_64), so one ~6.5 MB DMG covers every Mac on macOS 13+.

## 3. Channels

| Channel | Effort | Notes |
|---|---|---|
| **GitHub Releases** | low | Start here: attach the notarized DMG to the tag. |
| **Homebrew tap** (`brew install --cask apurvtaneja/tap/dictator`) | done | [apurvtaneja/homebrew-tap](https://github.com/apurvtaneja/homebrew-tap), `Casks/dictator.rb`. For each release, update `version` and `sha256` (`shasum -a 256 dist/Dictator-x.y.z.zip`). Once notarized, drop the "Open Anyway" caveat. |
| Official homebrew-cask | — | Needs a notarized app (Homebrew 5+ refuses casks that fail Gatekeeper) and ≥ 75 stars, 30 forks or 30 watchers. |
| Mac App Store | high | Possible in principle, but the sandbox blocks the global hotkey via Carbon and synthetic typing. Not worth it for v1. |
| Product Hunt, Show HN, r/macapps | — | For discovery, once a signed release exists. |

## 4. Automate releases (GitHub Actions)

On every pushed tag `v*`, a `macos-14` runner does the following:

1. installs Go and runs `make test` and `make VERSION=${tag#v}`;
2. imports the Developer ID certificate from repository secrets, signs, builds the DMG,
   notarizes and staples it;
3. uploads the DMG with `softprops/action-gh-release`;
4. bumps the cask's `version` and `sha256` in the tap.

## 5. Later

- **Check for updates:** a menu item that queries
  `api.github.com/repos/apurvtaneja/dictator/releases/latest` once, on demand. No background polling.
- **PAC proxy support:** evaluate PAC files through `CFNetworkCopyProxiesForURL`.
- **Offline mode:** an optional local speech-to-text engine (e.g. whisper.cpp with a small
  model) for users who can't send audio out. It would make the app bigger, so it should be
  a separate download.
