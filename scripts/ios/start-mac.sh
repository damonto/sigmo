#!/usr/bin/env bash
set -euo pipefail

# Parse the complete block before starting; children must not read a piped script.
{

usage() {
    cat <<'EOF'
Usage: bash start-mac.sh [SSH_TARGET] [--ssh-port PORT] [--source-port PORT] [--mcp-port PORT]

Start the outgoing SSH tunnel, then the Xcode gateway, in one terminal.
SSH_TARGET is the development host's SSH alias or user@hostname.
The HTTP bootstrap fills in SSH_TARGET and the source address automatically.
Defaults: source port 8766, MCP port 8765, SSH port from SSH config.
Ctrl-C stops both services. Rerun this command after a disconnect.
EOF
}

port_number() {
    if [[ ! $1 =~ ^[0-9]{1,5}$ ]] || (( 10#$1 < 1 || 10#$1 > 65535 )); then
        printf 'Invalid port: %s\n' "$1" >&2
        return 2
    fi
    printf '%s\n' "$((10#$1))"
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then usage; exit 0; fi
sigmo_ssh_target=${sigmo_default_ssh_target:-}
if [[ $# -gt 0 && ${1:-} != --* ]]; then
    sigmo_ssh_target=$1
    shift
fi
if [[ -z $sigmo_ssh_target || $sigmo_ssh_target == -* || $sigmo_ssh_target == *[[:space:]]* ]]; then
    printf 'SSH target must be a hostname, user@hostname, or SSH config alias\n' >&2
    exit 2
fi
sigmo_source_port=$(port_number "${sigmo_default_source_port:-8766}")
sigmo_source_host=${sigmo_default_source_host:-127.0.0.1}
sigmo_mcp_port=8765
sigmo_ssh_port=
while [[ $# -gt 0 ]]; do
    if [[ $# -lt 2 ]]; then usage >&2; exit 2; fi
    case "$1" in
        --ssh-port) sigmo_ssh_port=$(port_number "$2") ;;
        --source-port) sigmo_source_port=$(port_number "$2") ;;
        --mcp-port) sigmo_mcp_port=$(port_number "$2") ;;
        *) usage >&2; exit 2 ;;
    esac
    shift 2
done
if [[ $sigmo_source_port == "$sigmo_mcp_port" ]]; then
    printf 'Source and MCP ports must differ\n' >&2
    exit 2
fi

export PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"
for sigmo_command in ssh curl python uv; do
    if ! command -v "$sigmo_command" >/dev/null 2>&1; then
        printf '%s is not on PATH; activate your development environment first\n' "$sigmo_command" >&2
        exit 1
    fi
done
sigmo_service_dir=${SIGMO_MCP_DIR:-"$HOME/Library/Developer/SigmoMCP"}
sigmo_runtime_dir=$(mktemp -d "${TMPDIR:-/tmp}/sigmo-mcp.XXXXXX")
sigmo_tunnel_pid=
sigmo_gateway_pid=
sigmo_step_pid=

cleanup() {
    local sigmo_status=$? sigmo_target sigmo_pid sigmo_attempt sigmo_alive
    local sigmo_targets=("$sigmo_tunnel_pid" "${sigmo_gateway_pid:+-$sigmo_gateway_pid}" "${sigmo_step_pid:+-$sigmo_step_pid}")
    trap - EXIT
    trap '' INT TERM HUP
    # Only signal this invocation's SSH process and its own setup/gateway groups.
    for sigmo_target in "${sigmo_targets[@]}"; do
        if [[ -n $sigmo_target ]]; then kill -TERM -- "$sigmo_target" 2>/dev/null || true; fi
    done
    # Client session shutdown and the stdio EOF/TERM/KILL sequence need time to finish.
    for ((sigmo_attempt = 0; sigmo_attempt < 150; sigmo_attempt++)); do
        sigmo_alive=0
        for sigmo_target in "${sigmo_targets[@]}"; do
            if [[ -n $sigmo_target ]] && kill -0 -- "$sigmo_target" 2>/dev/null; then sigmo_alive=1; fi
        done
        if [[ $sigmo_alive == 0 ]]; then break; fi
        sleep 0.1
    done
    for sigmo_target in "${sigmo_targets[@]}"; do
        if [[ -n $sigmo_target ]]; then kill -KILL -- "$sigmo_target" 2>/dev/null || true; fi
    done
    for sigmo_pid in "$sigmo_tunnel_pid" "$sigmo_gateway_pid" "$sigmo_step_pid"; do
        if [[ -n $sigmo_pid ]]; then wait "$sigmo_pid" 2>/dev/null || true; fi
    done
    rm -rf -- "$sigmo_runtime_dir"
    exit "$sigmo_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

run_step() {
    # A separate group lets cleanup interrupt the whole setup step.
    set -m
    "$@" &
    sigmo_step_pid=$!
    set +m
    while kill -0 "$sigmo_step_pid" 2>/dev/null; do
        check_tunnel
        sleep 0.1
    done
    local sigmo_status=0
    wait "$sigmo_step_pid" || sigmo_status=$?
    if [[ $sigmo_status != 0 ]]; then return "$sigmo_status"; fi
    sigmo_step_pid=
}

check_tunnel() {
    if ! kill -0 "$sigmo_tunnel_pid" 2>/dev/null; then
        local sigmo_status=0
        wait "$sigmo_tunnel_pid" || sigmo_status=$?
        sigmo_tunnel_pid=
        printf 'SSH tunnel exited; rerun this script to reconnect.\n' >&2
        if [[ $sigmo_status == 0 ]]; then sigmo_status=1; fi
        exit "$sigmo_status"
    fi
}

# Reject an old local listener before it could be mistaken for our new forward.
python - "$sigmo_source_port" "$sigmo_mcp_port" <<'PY'
import socket
import sys
for port in map(int, sys.argv[1:]):
    with socket.socket() as listener:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            listener.bind(("127.0.0.1", port))
        except OSError as error:
            sys.exit(f"local port {port} is occupied; stop the old tunnel/gateway first: {error}")
PY

sigmo_ssh_args=(-NT
    -o StrictHostKeyChecking=ask -o ConnectTimeout=10 -o ExitOnForwardFailure=yes
    -o ServerAliveInterval=15 -o ServerAliveCountMax=3
    -o ControlMaster=no -o ControlPath=none -o ForkAfterAuthentication=no
    -R "127.0.0.1:$sigmo_mcp_port:127.0.0.1:$sigmo_mcp_port"
    -L "127.0.0.1:$sigmo_source_port:$sigmo_source_host:$sigmo_source_port")
if [[ -n $sigmo_ssh_port ]]; then sigmo_ssh_args+=(-p "$sigmo_ssh_port"); fi
printf 'Connecting SSH to %s...\n' "$sigmo_ssh_target"
ssh "${sigmo_ssh_args[@]}" "$sigmo_ssh_target" &
sigmo_tunnel_pid=$!

sigmo_source_url="http://127.0.0.1:$sigmo_source_port"
sigmo_deadline=$((SECONDS + 45))
until curl --noproxy '*' --fail --silent --show-error --connect-timeout 2 --max-time 2 \
    "$sigmo_source_url/manifest.json" -o "$sigmo_runtime_dir/manifest.json" 2>"$sigmo_runtime_dir/source-error"; do
    check_tunnel
    if (( SECONDS >= sigmo_deadline )); then
        printf 'Source service is not ready; start mcp_source.py on the development host.\n' >&2
        cat "$sigmo_runtime_dir/source-error" >&2
        exit 1
    fi
    sleep 1
done
check_tunnel
python - "$sigmo_runtime_dir/manifest.json" <<'PY'
import json
import sys
with open(sys.argv[1]) as source:
    manifest = json.load(source)
if not isinstance(manifest, dict) or "scripts/ios/mcp_gateway.py" not in manifest.get("files", {}):
    sys.exit("the forwarded endpoint is not a Sigmo source service")
PY

mkdir -p "$sigmo_service_dir"
run_step curl --noproxy '*' --fail --silent --show-error --connect-timeout 5 --max-time 30 \
    "$sigmo_source_url/mcp_gateway.py" -o "$sigmo_runtime_dir/mcp_gateway.py"
check_tunnel
mv "$sigmo_runtime_dir/mcp_gateway.py" "$sigmo_service_dir/mcp_gateway.py"

printf 'Starting Xcode gateway; developer MCP: http://127.0.0.1:%s/mcp\n' "$sigmo_mcp_port"
printf 'Keep this terminal open. Ctrl-C stops both services; rerun after a disconnect.\n'
# uv reads the gateway's inline dependencies and reuses its cached environment.
# Group ordinary descendants; the MCP client closes its separate bridge session.
set -m
uv run --no-python-downloads --python "$(command -v python)" --script "$sigmo_service_dir/mcp_gateway.py" \
    --source-url "$sigmo_source_url" --host 127.0.0.1 --port "$sigmo_mcp_port" \
    --workspace "$sigmo_service_dir/workspace" &
sigmo_gateway_pid=$!
set +m

# macOS's system Bash does not provide wait -n.
while kill -0 "$sigmo_gateway_pid" 2>/dev/null; do
    check_tunnel
    sleep 1
done
sigmo_status=0
wait "$sigmo_gateway_pid" || sigmo_status=$?
printf 'Xcode gateway exited; stopping its SSH tunnel.\n' >&2
exit "$sigmo_status"

} < /dev/null
