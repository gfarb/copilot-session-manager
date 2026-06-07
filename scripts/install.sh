#!/usr/bin/env bash
#
# Copilot Session Manager installer.
# Downloads the binary from the latest GitHub Release and installs the
# /session-manager slash-command extension.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gfarb/copilot-session-manager/main/scripts/install.sh | bash
#
# Env vars:
#   CSM_VERSION    Release tag to install (default: latest).
#   INSTALL_DIR    Where to put the binary (default: ~/.local/bin).
#   REPO           GitHub repo (default: gfarb/copilot-session-manager).

set -euo pipefail

REPO="${REPO:-gfarb/copilot-session-manager}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CSM_VERSION="${CSM_VERSION:-latest}"
EXT_DIR="$HOME/.copilot/extensions/csm"

bold() { printf "\033[1m%s\033[0m\n" "$*"; }
info() { printf "  %s\n" "$*"; }
err()  { printf "\033[31merror:\033[0m %s\n" "$*" >&2; }

detect_platform() {
    local os arch
    case "$(uname -s)" in
        Darwin) os=darwin ;;
        *)
            err "macOS-only today. Detected: $(uname -s)."
            err "Contributions for Linux/Windows support welcome: https://github.com/${REPO}"
            exit 1
            ;;
    esac
    case "$(uname -m)" in
        arm64) arch=arm64 ;;
        x86_64) arch=amd64 ;;
        *) err "unsupported macOS arch: $(uname -m)"; exit 1 ;;
    esac
    printf "%s-%s" "$os" "$arch"
}

resolve_tag() {
    if [[ "$CSM_VERSION" != "latest" ]]; then
        printf "%s" "$CSM_VERSION"
        return
    fi
    # Prefer gh if available (handles GHE auth too).
    if command -v gh >/dev/null 2>&1; then
        gh release view --repo "$REPO" --json tagName -q .tagName 2>/dev/null && return
    fi
    # Fall back to the public GitHub REST API.
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
        sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1
}

main() {
    bold "Copilot Session Manager - installer"

    local platform tag asset url tmpdir
    platform=$(detect_platform)
    tag=$(resolve_tag)
    if [[ -z "$tag" ]]; then
        err "could not resolve release tag. Set CSM_VERSION explicitly or check https://github.com/${REPO}/releases"
        exit 1
    fi
    asset="csm-${platform}.tar.gz"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
    info "repo:      $REPO"
    info "version:   $tag"
    info "platform:  $platform"
    info "binary:    $url"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    info "downloading..."
    if ! curl -fsSL "$url" -o "$tmpdir/release.tar.gz"; then
        err "download failed. Asset may not exist for $platform at $tag."
        exit 1
    fi
    if curl -fsSL "${url}.sha256" -o "$tmpdir/release.tar.gz.sha256" 2>/dev/null; then
        local expected actual sha_cmd
        expected=$(awk '{print $1}' "$tmpdir/release.tar.gz.sha256")
        if command -v shasum >/dev/null 2>&1; then
            sha_cmd="shasum -a 256"
        elif command -v sha256sum >/dev/null 2>&1; then
            sha_cmd="sha256sum"
        else
            err "no sha256 tool found (install shasum or sha256sum)"; exit 1
        fi
        actual=$($sha_cmd "$tmpdir/release.tar.gz" | awk '{print $1}')
        if [[ "$expected" != "$actual" ]]; then
            err "checksum mismatch: expected $expected, got $actual"
            exit 1
        fi
        info "checksum ok"
    else
        info "no .sha256 published; skipping verification"
    fi
    tar -xzf "$tmpdir/release.tar.gz" -C "$tmpdir"
    if [[ ! -f "$tmpdir/csm" ]]; then
        err "archive did not contain a 'csm' binary"
        exit 1
    fi

    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$tmpdir/csm" "$INSTALL_DIR/csm"
    info "installed binary: $INSTALL_DIR/csm"

    # Install the slash-command extension.
    mkdir -p "$EXT_DIR"
    curl -fsSL "https://raw.githubusercontent.com/${REPO}/${tag}/extension/extension.mjs" \
        -o "$EXT_DIR/extension.mjs"
    info "installed extension: $EXT_DIR/extension.mjs"

    bold "Done."
    info "1) Make sure $INSTALL_DIR is on your PATH."
    info "2) In Copilot CLI, type /session-manager to launch the TUI in a new terminal."
    info "3) Or run \`csm\` directly from any shell."
}

main "$@"
