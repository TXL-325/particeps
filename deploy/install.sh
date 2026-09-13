#!/bin/bash
set -Eeuo pipefail

# particeps installer: install / update / status / rollback / uninstall
# Downloads Agent only from GitHub Releases.

GITHUB_REPO="${PARTICEPS_GITHUB_REPO:-TXL-325/particeps}"
RELEASE_TAG="${PARTICEPS_RELEASE_TAG:-latest}"
ASSET_NAME="particeps-agent-linux-amd64"
CHECKSUM_NAME="SHA256SUMS"

AGENT_BIN_DIR="/opt/particeps"
AGENT_BINARY="/opt/particeps/particeps-agent"
AGENT_CONFIG_DIR="/etc/particeps"
AGENT_CONFIG_FILE="/etc/particeps/config.yaml"
AGENT_DATA_DIR="/var/lib/particeps"
AGENT_SERVICE="particeps-agent"
AGENT_UNIT_FILE="/etc/systemd/system/particeps-agent.service"
BACKUP_ROOT="/var/lib/particeps/backups"
LOCK_FILE="/run/lock/particeps-install.lock"
LOG_DIR="/var/log/particeps-install"
INCUS_PROJECT="particeps"
INCUS_NETWORK="particepsbr0"
INCUS_POOL="particeps-pool"
EXPECTED_IPV4="10.80.0.1/24"
POOL_SIZE=""

if [ -n "${PARTICEPS_TEST_ROOT:-}" ]; then
    PREFIX="${PARTICEPS_TEST_ROOT}"
    AGENT_BIN_DIR="$PREFIX/opt/particeps"
    AGENT_BINARY="$AGENT_BIN_DIR/particeps-agent"
    AGENT_CONFIG_DIR="$PREFIX/etc/particeps"
    AGENT_CONFIG_FILE="$AGENT_CONFIG_DIR/config.yaml"
    AGENT_DATA_DIR="$PREFIX/var/lib/particeps"
    AGENT_UNIT_FILE="$PREFIX/etc/systemd/system/particeps-agent.service"
    BACKUP_ROOT="$AGENT_DATA_DIR/backups"
    LOCK_FILE="$PREFIX/run/lock/particeps-install.lock"
    LOG_DIR="$PREFIX/var/log/particeps-install"
    SKIP_HOST=1
else
    SKIP_HOST=0
fi

NON_INTERACTIVE="${NON_INTERACTIVE:-0}"
UPDATE_ONLY=0
KEEP_INSTANCES=0
CONFIRM_WORD=""
ACTION=""
ORIGINAL_ARGC=$#
UPGRADE_BACKUP=""
TASK_STEP="准备操作"
TASK_STATE="尚未修改程序、服务或 Incus 资源"
TASK_ERROR=""
TASK_RETRY=1
TASK_WORK_DIR=""
TASK_RESULT_DIR=""
TASK_LOG=""
TASK_LOG_PID=""
MENU_SESSION_DIR=""
MENU_INTERRUPTED=0
MENU_EOF=0
RESULT_RETRY=0

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >&2; }
die() { TASK_ERROR="$*"; log "$TASK_ERROR"; exit 1; }
cancel_task() { TASK_ERROR="已取消当前操作"; exit "${1:-130}"; }

step() {
    TASK_STEP="$*"
    log "[$TASK_STEP]"
}

task_state() {
    TASK_STATE="$1"
    TASK_RETRY="${2:-0}"
}

unexpected_error() {
    local code="$1" line="$2"
    TASK_ERROR="命令执行失败（退出码 ${code}，脚本第 ${line} 行）"
    log "$TASK_ERROR"
}

finish_task() {
    local code="$1"
    trap - EXIT ERR
    trap '' INT TERM
    set +e
    if [ -n "$TASK_WORK_DIR" ]; then
        rm -rf -- "$TASK_WORK_DIR" || log "临时文件清理失败: $TASK_WORK_DIR"
    fi
    if [ "$code" -ne 0 ]; then
        log "${TASK_ERROR:-操作未完成（退出码 $code）}；阶段: $TASK_STEP"
        log "当前状态: $TASK_STATE"
    fi
    if [ -n "$TASK_RESULT_DIR" ]; then
        printf '%s\n' "$TASK_STEP" > "$TASK_RESULT_DIR/step"
        printf '%s\n' "$TASK_STATE" > "$TASK_RESULT_DIR/state"
        printf '%s\n' "$TASK_ERROR" > "$TASK_RESULT_DIR/error"
        printf '%s\n' "$TASK_RETRY" > "$TASK_RESULT_DIR/retry"
        printf '%s\n' "$TASK_LOG" > "$TASK_RESULT_DIR/log"
    fi
    if [ -n "$TASK_LOG_PID" ]; then
        exec 2>&8 8>&-
        wait "$TASK_LOG_PID" || true
    fi
    exit "$code"
}

require_root() {
    [ "$(id -u)" = "0" ] || die "需要 root"
}

download_base() {
    if [ "$RELEASE_TAG" = "latest" ]; then
        echo "https://github.com/${GITHUB_REPO}/releases/latest/download"
    else
        echo "https://github.com/${GITHUB_REPO}/releases/download/${RELEASE_TAG}"
    fi
}

show_help() {
    cat <<'EOF'
particeps 安装脚本（仅从 GitHub Release 下载二进制）

  bash install.sh                         终端菜单
  bash install.sh --update-only           仅升级 Agent（需已安装）
  bash install.sh --status                显示服务与最近备份
  bash install.sh --rollback              从最近升级备份恢复
  bash install.sh --uninstall             默认全部卸载（需 --confirm PURGE）
  bash install.sh --uninstall --keep-instances
                                          只卸 Agent，保留实例与数据
  bash install.sh --tag vX.Y.Z            使用指定 Release
  bash install.sh -y                      非交互（存储池用默认大小；不卸 Incus）

菜单操作失败后按回车返回，Ctrl+C 取消当前任务；0 退出主菜单。
诊断日志：/var/log/particeps-install（不记录初始管理员密码）。
环境变量：PARTICEPS_RELEASE_TAG、PARTICEPS_GITHUB_REPO
EOF
}

show_menu() {
    printf '\nparticeps 安装脚本\n'
    printf '  1) 安装/升级\n'
    printf '  2) 状态\n'
    printf '  3) 回滚最近备份\n'
    printf '  4) 全部卸载（要输入 PURGE）\n'
    printf '  5) 只卸 Agent\n'
    printf '  0) 退出\n'
    printf '  Ctrl+C 取消当前输入或任务\n'
}

menu_read() {
    MENU_INTERRUPTED=0
    # A flag-only SIGINT trap lets Bash resume its read builtin. Return from
    # this input helper explicitly, then restore the task/log-view handler.
    trap 'MENU_INTERRUPTED=1; trap "MENU_INTERRUPTED=1" INT; printf "\n已取消当前输入\n"; return 1' INT
    if read -r -p "$1" "$2"; then
        trap 'MENU_INTERRUPTED=1' INT
        return 0
    fi
    trap 'MENU_INTERRUPTED=1' INT
    if [ "$MENU_INTERRUPTED" = "1" ]; then
        printf '\n已取消当前输入\n'
    else
        MENU_EOF=1
        printf '\n输入已结束\n'
    fi
    return 1
}

edit_release_tag() {
    local tag
    while true; do
        menu_read "Release 版本 [${RELEASE_TAG}]（0 返回）: " tag || return 1
        [ "$tag" != "0" ] || return 1
        tag="${tag:-$RELEASE_TAG}"
        if [[ "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
            RELEASE_TAG="$tag"
            return 0
        fi
        printf '版本只能包含字母、数字、点、下划线和短横线，例如 latest 或 v0.1.1。\n'
    done
}

action_summary() {
    local choice
    case "$ACTION" in
        status) return 0 ;;
        install)
            if installed; then
                printf '\n操作：仅升级 Agent；保留实例与配置，替换前创建备份。\n'
            else
                printf '\n操作：首次安装 Agent，并准备专用 Incus 资源。\n'
            fi
            ;;
        rollback) printf '\n操作：恢复最近升级备份中的程序、单元、配置与管理数据库。\n' ;;
        uninstall)
            if [ "$KEEP_INSTANCES" = "1" ]; then
                printf '\n操作：只卸 Agent；实例、配置与数据保留。\n'
            else
                printf '\n操作：全部卸载 particeps 项目内实例、专用资源、程序、配置与数据。\n'
                return 0 # The task requires the existing PURGE confirmation.
            fi
            ;;
    esac
    while true; do
        if [ "$ACTION" = "install" ]; then
            printf 'Release：%s\n' "$RELEASE_TAG"
            menu_read '回车执行，E 修改版本，0 返回菜单: ' choice || return 1
        else
            menu_read '回车执行，0 返回菜单: ' choice || return 1
        fi
        case "$choice" in
            '') return 0 ;;
            0) return 1 ;;
            E|e)
                if [ "$ACTION" = "install" ]; then
                    edit_release_tag || return 1
                else
                    printf '无效选项，请重新输入。\n'
                fi
                ;;
            *) printf '无效选项，请重新输入。\n' ;;
        esac
    done
}

show_result() {
    local code="$1" detail state logfile retry choice
    RESULT_RETRY=0
    detail=$(cat "$TASK_RESULT_DIR/step" 2>/dev/null || printf '准备操作')
    state=$(cat "$TASK_RESULT_DIR/state" 2>/dev/null || printf '结果未能完整记录，请检查实际状态')
    logfile=$(cat "$TASK_RESULT_DIR/log" 2>/dev/null || true)
    retry=$(cat "$TASK_RESULT_DIR/retry" 2>/dev/null || printf 0)
    case "$code" in
        0) printf '\n[成功] 操作完成\n' ;;
        130|143) printf '\n[已取消] 当前任务已停止\n' ;;
        *)
            printf '\n[失败] %s\n' "$detail"
            printf '原因：%s\n' "$(cat "$TASK_RESULT_DIR/error" 2>/dev/null || true)"
            ;;
    esac
    printf '当前状态：%s\n' "$state"
    [ -z "$logfile" ] || printf '诊断日志：%s\n' "$logfile"
    case "$code" in 130|143) return 0 ;; esac
    while true; do
        printf '\n按回车返回菜单，L 查看日志'
        if [ "$code" -ne 0 ] && [ "$retry" = "1" ] && [ "$ACTION" != "uninstall" ]; then
            printf '，R 重新检查并重试'
            [ "$ACTION" != "install" ] || printf '，E 修改版本后重试'
        fi
        printf '\n'
        menu_read '> ' choice || return 0
        case "$choice" in
            ''|0) return 0 ;;
            L|l)
                if [ -n "$logfile" ] && [ -f "$logfile" ]; then
                    if ! cat -- "$logfile"; then
                        if [ "$MENU_INTERRUPTED" = "1" ]; then
                            printf '\n已停止查看日志\n'
                            return 0
                        fi
                        printf '无法读取诊断日志，请检查文件状态。\n'
                    fi
                else
                    printf '没有可用的诊断日志。\n'
                fi
                ;;
            R|r|E|e)
                if [ "$code" -ne 0 ] && [ "$retry" = "1" ] && [ "$ACTION" != "uninstall" ]; then
                    if [[ "$choice" = [Ee] ]]; then
                        [ "$ACTION" = "install" ] || continue
                        if ! edit_release_tag; then
                            [ "$MENU_EOF" = "0" ] || return 0
                            continue
                        fi
                    fi
                    RESULT_RETRY=1
                    return 0
                fi
                printf '当前操作不提供直接重试，请返回菜单核对状态。\n'
                ;;
            *) printf '无效选项，请重新输入。\n' ;;
        esac
    done
}

menu_loop() {
    local choice code
    MENU_SESSION_DIR=$(mktemp -d)
    trap 'rm -rf -- "$MENU_SESSION_DIR"' EXIT
    trap 'MENU_INTERRUPTED=1' INT
    while [ "$MENU_EOF" = "0" ]; do
        ACTION=""
        UPDATE_ONLY=0
        KEEP_INSTANCES=0
        CONFIRM_WORD=""
        show_menu
        if ! menu_read '> ' choice; then
            continue
        fi
        case "$choice" in
            1) ACTION="install" ;;
            2) ACTION="status" ;;
            3) ACTION="rollback" ;;
            4) ACTION="uninstall" ;;
            5) ACTION="uninstall"; KEEP_INSTANCES=1 ;;
            0) return 0 ;;
            *) printf '无效选项，请重新输入。\n'; continue ;;
        esac
        if ! action_summary; then
            continue
        fi
        while true; do
            TASK_RESULT_DIR=$(mktemp -d "$MENU_SESSION_DIR/result-XXXXXX")
            # Do not put this subshell in an if/|| condition: Bash would disable
            # errexit inside every task function and allow failed steps to continue.
            set +e
            (
                set -Eeuo pipefail
                execute_action
            )
            code=$?
            set -e
            show_result "$code"
            rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
            [ "$RESULT_RETRY" = "1" ] || break
        done
    done
}

while [ $# -gt 0 ]; do
    case "$1" in
        --update-only) UPDATE_ONLY=1; ACTION="install"; shift ;;
        --status) ACTION="status"; shift ;;
        --rollback) ACTION="rollback"; shift ;;
        --uninstall) ACTION="uninstall"; shift ;;
        --keep-instances) KEEP_INSTANCES=1; shift ;;
        --tag) [ $# -ge 2 ] || die "--tag 需要值"; RELEASE_TAG="$2"; shift 2 ;;
        --confirm) [ $# -ge 2 ] || die "--confirm 需要值"; CONFIRM_WORD="$2"; shift 2 ;;
        -y|--yes|--non-interactive) NON_INTERACTIVE=1; shift ;;
        -h|--help) show_help; exit 0 ;;
        *) die "未知参数: $1" ;;
    esac
done

acquire_lock() {
    exec 9>"$LOCK_FILE"
    flock -n 9 || die "已有安装/升级在运行"
}

host_preflight() {
    [ "$(uname -m)" = "x86_64" ] || die "需要 amd64"
    if [ ! -f /etc/os-release ] || ! grep -q 'VERSION_ID="13"' /etc/os-release; then
        die "需要 Debian 13"
    fi
}

installed() {
    [ -f "$AGENT_BINARY" ] && [ -f "$AGENT_CONFIG_FILE" ] && [ -f "$AGENT_UNIT_FILE" ]
}

state_db_path() {
    local dir
    dir=$(sed -n 's/^data_dir:[[:space:]]*//p' "$AGENT_CONFIG_FILE" 2>/dev/null | tr -d '"'"'" | tail -n1)
    [ -n "$dir" ] || dir="$AGENT_DATA_DIR"
    echo "$dir/state.db"
}

metrics_db_path() {
    local dir
    dir=$(sed -n 's/^data_dir:[[:space:]]*//p' "$AGENT_CONFIG_FILE" 2>/dev/null | tr -d '"'"'" | tail -n1)
    [ -n "$dir" ] || dir="$AGENT_DATA_DIR"
    echo "$dir/metrics.db"
}

listen_addr() {
    sed -n 's/^listen:[[:space:]]*//p' "$AGENT_CONFIG_FILE" 2>/dev/null | tail -n1
}

listen_port() {
    local listen port
    listen=$(listen_addr)
    port="${listen##*:}"
    case "$port" in
        ''|*[!0-9]*) echo 8792 ;;
        *) echo "$port" ;;
    esac
}

root_avail_g() {
    df -BG / | awk 'NR==2 {g=$4; gsub(/G/,"",g); g=int(g); if(g<0)g=0; print g}'
}

default_pool_g() {
    local avail="$1"
    awk -v a="$avail" 'BEGIN{
        v=int(a*0.30)
        if (v<1) v=1
        if (v>25) v=25
        if (v>a) v=a
        print v
    }'
}

prompt_pool_size() {
    local avail default size confirm
    avail=$(root_avail_g)
    [ "${avail:-0}" -ge 1 ] || die "根分区至少需要 1G 可用（当前 ${avail:-0}G）"
    default=$(default_pool_g "$avail")
    if [ -n "$MENU_SESSION_DIR" ] && [ -f "$MENU_SESSION_DIR/pool-size" ]; then
        read -r size < "$MENU_SESSION_DIR/pool-size" || true
        if [[ "$size" =~ ^[1-9][0-9]*$ ]] && [ "${#size}" -le "${#avail}" ] && [ "$size" -le "$avail" ]; then
            default="$size"
        fi
    fi
    if [ "$NON_INTERACTIVE" = "1" ]; then
        POOL_SIZE="${default}GiB"
        log "存储池大小 ${default}G（默认，可用 ${avail}G）"
        return
    fi
    while true; do
        size=""
        read -rp "设置存储池大小[单位G][默认:${default}G]（0 取消）: " size || die "无法读取存储池大小"
        [ "$size" != "0" ] || cancel_task
        if [ -z "$size" ]; then
            size="$default"
        elif ! [[ "$size" =~ ^[1-9][0-9]*$ ]]; then
            echo "无效，请重新输入"
            continue
        elif [ "${#size}" -gt "${#avail}" ] || [ "$size" -gt "$avail" ]; then
            echo "请输入 1 至 ${avail} 的整数"
            continue
        fi
        while true; do
            confirm=""
            read -rp "确认将存储池大小设为 ${size}G？[Y/n]: " confirm || die "无法读取确认"
            case "$confirm" in
                ""|Y|y)
                    POOL_SIZE="${size}GiB"
                    if [ -n "$MENU_SESSION_DIR" ]; then
                        printf '%s\n' "$size" > "$MENU_SESSION_DIR/pool-size"
                    fi
                    log "存储池大小 ${size}G"
                    return
                    ;;
                N|n) break ;;
                *) echo "无效，请重新输入" ;;
            esac
        done
    done
}

sqlite_backup() {
    local src="$1" dest="$2"
    python3 - "$src" "$dest" <<'PY'
import pathlib, sqlite3, sys
src, dest = sys.argv[1], sys.argv[2]
with sqlite3.connect(pathlib.Path(src).as_uri() + "?mode=ro", uri=True, timeout=30) as source:
    with sqlite3.connect(dest) as target:
        source.backup(target)
        if target.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
            raise SystemExit("database backup failed integrity check")
PY
}

admin_exists() {
    local db
    db=$(state_db_path)
    [ -f "$db" ] || return 1
    python3 - "$db" <<'PY'
import sqlite3, sys
try:
    con = sqlite3.connect(sys.argv[1])
    n = con.execute("SELECT COUNT(*) FROM admin").fetchone()[0]
    sys.exit(0 if n > 0 else 1)
except Exception:
    sys.exit(1)
PY
}

generate_password() {
    python3 - <<'PY'
import secrets, string
alphabet = string.ascii_letters + string.digits
print("".join(secrets.choice(alphabet) for _ in range(20)))
PY
}

show_status() {
    local agent_state="inactive" incus_state="n/a" backup="none" listen="n/a" sha="n/a"
    if command -v systemctl >/dev/null 2>&1; then
        agent_state=$(systemctl is-active "$AGENT_SERVICE" 2>/dev/null || true)
        incus_state=$(systemctl is-active incus 2>/dev/null || systemctl is-active incus.service 2>/dev/null || true)
    fi
    [ -f "$AGENT_CONFIG_FILE" ] && listen=$(listen_addr)
    [ -f "$AGENT_BINARY" ] && command -v sha256sum >/dev/null 2>&1 && sha=$(sha256sum "$AGENT_BINARY" | awk '{print $1}')
    if [ -f "$BACKUP_ROOT/latest" ]; then
        backup=$(cat "$BACKUP_ROOT/latest")
    fi
    printf '%s\n' \
        "AGENT_SERVICE=${agent_state:-inactive}" \
        "INCUS_SERVICE=${incus_state:-n/a}" \
        "LISTEN=${listen:-n/a}" \
        "BACKUP=${backup}" \
        "BINARY_SHA256=${sha}"
}

write_default_config() {
    mkdir -p "$AGENT_CONFIG_DIR"
    chmod 0750 "$AGENT_CONFIG_DIR" 2>/dev/null || true
    cat > "$AGENT_CONFIG_FILE" <<EOF
listen: 0.0.0.0:8792
session_cookie_secure: false
data_dir: ${AGENT_DATA_DIR}
incus_socket: /var/lib/incus/unix.socket
incus_project: particeps
storage_pool: particeps-pool
network: particepsbr0
private_ipv4_cidr: 10.80.0.0/24
task_concurrency: 2
cpu_cap_cores: 0
port_pool_start: 20000
port_pool_end: 59999
ports_per_guest: 20
source_ip_limit: 0
sample_seconds: 5
EOF
    chmod 0640 "$AGENT_CONFIG_FILE"
}

write_unit() {
    mkdir -p "$(dirname "$AGENT_UNIT_FILE")"
    cat > "$AGENT_UNIT_FILE" <<EOF
[Unit]
Description=particeps host agent
After=network.target incus.service
Wants=incus.service

[Service]
Type=simple
ExecStart=${AGENT_BINARY} --config ${AGENT_CONFIG_FILE}
Restart=on-failure
RestartSec=3
User=root
NoNewPrivileges=true
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    chmod 0644 "$AGENT_UNIT_FILE"
}

verify_elf64() {
    python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path, "rb") as fh:
    hdr = fh.read(20)
if len(hdr) < 5 or hdr[:4] != b"\x7fELF" or hdr[4] != 2:
    raise SystemExit(f"{path} is not an ELF64 binary")
PY
}

download_agent() {
    local dest="$1" base tmp
    base=$(download_base)
    tmp=$(mktemp -d "$TASK_WORK_DIR/download-XXXXXX")
    log "下载 ${base}/${ASSET_NAME}"
    if ! curl -fsSL -o "$tmp/$ASSET_NAME" "${base}/${ASSET_NAME}"; then
        rm -rf "$tmp"
        die "下载 Agent 失败；现有程序未改动"
    fi
    if ! curl -fsSL -o "$tmp/$CHECKSUM_NAME" "${base}/${CHECKSUM_NAME}"; then
        rm -rf "$tmp"
        die "下载 SHA256SUMS 失败；现有程序未改动"
    fi
    if ! ( cd "$tmp" && grep -E "[[:space:]]${ASSET_NAME}\$" "$CHECKSUM_NAME" | sha256sum -c - >/dev/null ); then
        rm -rf "$tmp"
        die "SHA256 校验失败；现有程序未改动"
    fi
    if ! verify_elf64 "$tmp/$ASSET_NAME"; then
        rm -rf "$tmp"
        die "不是 ELF64 二进制；现有程序未改动"
    fi
    mkdir -p "$(dirname "$dest")"
    cp "$tmp/$ASSET_NAME" "$dest"
    chmod 0755 "$dest"
    rm -rf "$tmp"
}

backup_existing() {
    local db metrics
    mkdir -p "$BACKUP_ROOT"
    chmod 0700 "$BACKUP_ROOT"
    UPGRADE_BACKUP=$(mktemp -d "$BACKUP_ROOT/upgrade-$(date +%Y%m%d-%H%M%S)-XXXXXX")
    chmod 0700 "$UPGRADE_BACKUP"
    cp -p "$AGENT_BINARY" "$UPGRADE_BACKUP/particeps-agent"
    cp -p "$AGENT_CONFIG_FILE" "$UPGRADE_BACKUP/config.yaml"
    cp -p "$AGENT_UNIT_FILE" "$UPGRADE_BACKUP/particeps-agent.service"
    db=$(state_db_path)
    [ -f "$db" ] || die "管理库不存在: $db；升级中止"
    sqlite_backup "$db" "$UPGRADE_BACKUP/state.db"
    metrics=$(metrics_db_path)
    if [ -f "$metrics" ]; then
        sqlite_backup "$metrics" "$UPGRADE_BACKUP/metrics.db"
    fi
    chmod 0600 "$UPGRADE_BACKUP"/* 2>/dev/null || true
    echo "$UPGRADE_BACKUP" > "$BACKUP_ROOT/latest"
    log "升级前备份: $UPGRADE_BACKUP"
}

preflight_existing() {
    installed || die "未找到标准安装，拒绝升级"
    command -v python3 >/dev/null || die "需要 python3 以备份管理库"
    command -v curl >/dev/null || die "需要 curl 以下载 Release"
    command -v sha256sum >/dev/null || die "需要 sha256sum"
    grep -q "$AGENT_BINARY" "$AGENT_UNIT_FILE" || die "非标准 ExecStart 路径，需手工处理"
    grep -q "$AGENT_CONFIG_FILE" "$AGENT_UNIT_FILE" || die "非标准配置路径，需手工处理"
    [ -f "$(state_db_path)" ] || die "管理库不存在；升级中止"
}

incus_kv() {
    incus "$1" get "$2" "$3" 2>/dev/null || true
}

host_has_ipv6() {
    command -v ip >/dev/null 2>&1 || return 1
    local iface
    while read -r iface; do
        [ -n "$iface" ] || continue
        case "$iface" in
            lo|lo:*|docker*|br-*|veth*|incus*|lxc*|particeps*|virbr*) continue ;;
        esac
        return 0
    done < <(ip -6 -o addr show scope global 2>/dev/null | awk '{print $2}')
    return 1
}

skip_iface() {
    case "$1" in
        lo|lo:*|docker*|br-*|veth*|incus*|lxc*|particeps*|virbr*) return 0 ;;
        *) return 1 ;;
    esac
}

print_panel_urls() {
    local port iface ipaddr
    port=$(listen_port)
    if ! command -v ip >/dev/null 2>&1; then
        echo "  （未检测到在用网卡 IPv4）"
        return
    fi
    local found=0
    while read -r iface ipaddr; do
        [ -n "$iface" ] && [ -n "$ipaddr" ] || continue
        skip_iface "$iface" && continue
        echo "  http://${ipaddr}:${port}"
        found=1
    done < <(ip -4 -o addr show up 2>/dev/null | awk '{
        iface=$2
        for (i=1; i<=NF; i++) if ($i=="inet") {
            ip=$(i+1); sub(/\/.*/, "", ip); print iface, ip
        }
    }')
    [ "$found" = "1" ] || echo "  （未检测到在用网卡 IPv4）"
}

print_finish() {
    local pass="$1" existing="$2"
    echo
    echo "安装完成"
    echo
    echo "面板地址"
    print_panel_urls
    echo
    if [ "$existing" = "1" ]; then
        echo "管理员已存在，未生成新密码。"
    else
        echo "初始密码: ${pass}"
        echo "请立即保存。密码不会写入磁盘。"
    fi
}

init_admin() {
    INSTALL_ADMIN_EXISTING=0
    INSTALL_ADMIN_PASSWORD=""
    if admin_exists; then
        INSTALL_ADMIN_EXISTING=1
        return
    fi
    INSTALL_ADMIN_PASSWORD=$(generate_password)
    [ -n "$INSTALL_ADMIN_PASSWORD" ] || die "生成管理员密码失败"
    if [ "$SKIP_HOST" != "1" ]; then
        if ! PARTICEPS_ADMIN_PASSWORD="$INSTALL_ADMIN_PASSWORD" "$AGENT_BINARY" --config "$AGENT_CONFIG_FILE" --bootstrap-only; then
            die "写入管理员密码失败"
        fi
    fi
}

ensure_incus_resources() {
    if incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
        local driver
        driver=$(incus storage show "$INCUS_POOL" | awk '/^driver:/ {print $2; exit}')
        [ "$driver" = "lvm" ] || die "存储池 ${INCUS_POOL} 已存在但驱动不是 lvm，拒绝修改"
        log "存储池 ${INCUS_POOL} 已存在，不修改"
    else
        [ -n "$POOL_SIZE" ] || die "未设置存储池大小"
        incus storage create "$INCUS_POOL" lvm size="$POOL_SIZE" lvm.use_thinpool=true
    fi

    local want_v6=0
    if host_has_ipv6; then
        want_v6=1
    fi

    if incus network show "$INCUS_NETWORK" >/dev/null 2>&1; then
        local addr nat v6
        addr=$(incus_kv network "$INCUS_NETWORK" ipv4.address)
        nat=$(incus_kv network "$INCUS_NETWORK" ipv4.nat)
        v6=$(incus_kv network "$INCUS_NETWORK" ipv6.address)
        [ "$addr" = "$EXPECTED_IPV4" ] || die "网桥 ${INCUS_NETWORK} 已存在但 ipv4.address=${addr}，期望 ${EXPECTED_IPV4}，拒绝修改"
        [ "$nat" = "true" ] || die "网桥 ${INCUS_NETWORK} 已存在但 ipv4.nat=${nat}，拒绝修改"
        if [ "$want_v6" = "1" ] && { [ -z "$v6" ] || [ "$v6" = "none" ]; }; then
            incus network set "$INCUS_NETWORK" ipv6.address=auto ipv6.nat=true
            log "网桥 ${INCUS_NETWORK} 已接入 IPv6"
        else
            log "网桥 ${INCUS_NETWORK} 已存在，不修改"
        fi
    else
        if [ "$want_v6" = "1" ]; then
            incus network create "$INCUS_NETWORK" ipv4.address="$EXPECTED_IPV4" ipv4.nat=true ipv6.address=auto ipv6.nat=true
            log "已创建网桥 ${INCUS_NETWORK}（IPv4 NAT + IPv6）"
        else
            incus network create "$INCUS_NETWORK" ipv4.address="$EXPECTED_IPV4" ipv4.nat=true ipv6.address=none
            log "已创建网桥 ${INCUS_NETWORK}（IPv4 NAT，无 IPv6）"
        fi
    fi

    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        log "项目 ${INCUS_PROJECT} 已存在，不修改"
    else
        incus project create "$INCUS_PROJECT" -c features.images=true -c features.profiles=true -c features.storage.volumes=true
    fi
}

setup_cgroup() {
    mkdir -p /sys/fs/cgroup/particeps-guests || true
    echo '+cpu' > /sys/fs/cgroup/particeps-guests/cgroup.subtree_control 2>/dev/null || true
    local n quota
    n=$(nproc 2>/dev/null || echo 1)
    case "$n" in
        ''|*[!0-9]*) n=1 ;;
    esac
    [ "$n" -ge 1 ] || n=1
    quota=$(awk -v n="$n" 'BEGIN{
        c=n*0.75
        if (c<0.25) c=0.25
        c=int(c*100+0.5)/100
        q=int(c*100000+0.5)
        if (q<1000) q=1000
        print q
    }')
    echo "$quota 100000" > /sys/fs/cgroup/particeps-guests/cpu.max 2>/dev/null || true
}

install_packages() {
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y incus-base incus-client lvm2 thin-provisioning-tools uidmap conntrack python3 ca-certificates curl
    systemctl enable --now incus.service incus.socket 2>/dev/null || systemctl enable --now incus
}

do_rollback() {
    step '1/3 检查回滚备份'
    acquire_lock
    local target
    target="${1:-}"
    [ -n "$target" ] || target=$(cat "$BACKUP_ROOT/latest" 2>/dev/null || true)
    [ -n "$target" ] && [ -d "$target" ] || die "未找到升级备份"
    [ -f "$target/particeps-agent" ] || die "备份缺少程序"
    [ -f "$target/config.yaml" ] || die "备份缺少配置"
    [ -f "$target/particeps-agent.service" ] || die "备份缺少单元文件"
    [ -f "$target/state.db" ] || die "备份缺少管理库"
    step '2/3 恢复程序、配置与管理数据'
    task_state "已开始恢复文件，可能部分完成；备份: $target"
    mkdir -p "$AGENT_BIN_DIR" "$AGENT_CONFIG_DIR" "$(dirname "$AGENT_UNIT_FILE")" "$AGENT_DATA_DIR"
    cp -p "$target/particeps-agent" "$AGENT_BINARY"
    chmod 0755 "$AGENT_BINARY"
    cp -p "$target/config.yaml" "$AGENT_CONFIG_FILE"
    chmod 0640 "$AGENT_CONFIG_FILE"
    cp -p "$target/particeps-agent.service" "$AGENT_UNIT_FILE"
    cp -p "$target/state.db" "$(state_db_path)"
    if [ -f "$target/metrics.db" ]; then
        cp -p "$target/metrics.db" "$(metrics_db_path)"
    fi
    if command -v systemctl >/dev/null 2>&1; then
        step '3/3 核对服务状态'
        systemctl daemon-reload 2>/dev/null || true
        if systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
            systemctl restart "$AGENT_SERVICE" || die "回滚后重启失败。备份: $target"
            verify_active_service
        fi
    fi
    task_state "已恢复备份: $target"
    log "已从 $target 回滚"
}

confirm_purge() {
    if [ "$NON_INTERACTIVE" = "1" ]; then
        [ "$CONFIRM_WORD" = "PURGE" ] || die "非交互全部卸载需要 --confirm PURGE"
        return
    fi
    local answer
    read -rp "输入 PURGE 继续全部卸载（其他输入取消）: " answer || cancel_task
    [ "$answer" = "PURGE" ] || cancel_task
}

instance_names() {
    local proj="$1"
    incus --project "$proj" list --format csv -c n 2>/dev/null | sed '/^$/d' || true
}

delete_particeps_instances() {
    command -v incus >/dev/null 2>&1 || return 0
    incus project show "$INCUS_PROJECT" >/dev/null 2>&1 || return 0
    local attempt names name remaining
    names=$(instance_names "$INCUS_PROJECT")
    log "将强制删除项目 ${INCUS_PROJECT} 中的实例: ${names:-<none>}"
    for attempt in 1 2 3; do
        names=$(instance_names "$INCUS_PROJECT")
        [ -z "$names" ] && return 0
        while IFS= read -r name; do
            [ -n "$name" ] || continue
            incus --project "$INCUS_PROJECT" stop --force "$name" >/dev/null 2>&1 || true
            if ! incus --project "$INCUS_PROJECT" delete --force "$name" >/dev/null 2>&1; then
                log "强制删除失败: $name（第 ${attempt} 次）"
            fi
        done <<< "$names"
    done
    remaining=$(instance_names "$INCUS_PROJECT")
    [ -z "$remaining" ] || die "实例未能删干净，剩余: $remaining"
}

delete_particeps_resources() {
    command -v incus >/dev/null 2>&1 || return 0
    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        incus --project "$INCUS_PROJECT" profile device remove default eth0 >/dev/null 2>&1 || true
        incus --project "$INCUS_PROJECT" profile device remove default root >/dev/null 2>&1 || true
    fi
    if incus network show "$INCUS_NETWORK" >/dev/null 2>&1; then
        incus network delete "$INCUS_NETWORK" || die "删除网桥 ${INCUS_NETWORK} 失败；可能仍被其他项目使用"
    fi
    if incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
        incus storage delete "$INCUS_POOL" || die "删除存储池 ${INCUS_POOL} 失败"
    fi
    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        incus project delete "$INCUS_PROJECT" || die "删除项目 ${INCUS_PROJECT} 失败"
    fi
}

report_foreign_instances() {
    command -v incus >/dev/null 2>&1 || return 1
    local proj names name status found=0
    while IFS= read -r proj; do
        proj="${proj%%,*}"
        proj="${proj//$'\r'/}"
        [ -n "$proj" ] || continue
        [ "$proj" = "$INCUS_PROJECT" ] && continue
        names=$(incus --project "$proj" list --format csv -c n,s 2>/dev/null || true)
        while IFS= read -r line; do
            [ -n "$line" ] || continue
            name="${line%%,*}"
            status="${line#*,}"
            [ -n "$name" ] || continue
            if [ "$found" = "0" ]; then
                echo "其它项目中仍有实例："
                found=1
            fi
            echo "  项目=${proj} 实例=${name} 状态=${status}"
        done <<< "$names"
    done < <(incus project list --format csv -c n 2>/dev/null || true)
    if [ "$found" = "0" ]; then
        echo "其它项目没有实例。"
        return 1
    fi
    echo "若删除 Incus，上述实例也会被清除。"
    return 0
}

purge_incus() {
    log "开始卸载 Incus"
    task_state 'particeps 已卸载，正在清除 Incus；包或数据清理可能部分完成'
    systemctl stop incus incus.socket incus-user incus-user.socket 2>/dev/null || true
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get purge -y incus incus-base incus-client incus-extra || die "卸载 Incus 包失败，未继续删除数据目录"
        apt-get autoremove -y || die "清理 Incus 依赖失败，未继续删除数据目录"
        apt-get purge -y lxcfs || die "卸载 lxcfs 失败，未继续删除数据目录"
    fi
    umount /var/lib/lxcfs 2>/dev/null || true
    rm -rf /var/lib/incus /var/log/incus /etc/incus /run/incus /var/cache/incus /var/lib/lxcfs
    task_state 'particeps 与 Incus 已卸载并清理数据目录'
    log "Incus 已卸载并清理数据目录"
}

prompt_remove_incus() {
    command -v incus >/dev/null 2>&1 || { log "未找到 incus 命令，跳过"; return; }
    report_foreign_instances || true
    if [ "$NON_INTERACTIVE" = "1" ]; then
        log "非交互模式：保留 Incus"
        return
    fi
    local answer
    while true; do
        read -rp "是否删除 Incus 并清理干净？[y/N]: " answer || { log "保留 Incus"; return; }
        case "$answer" in
            Y|y) purge_incus; return ;;
            N|n|"") log "保留 Incus"; return ;;
            *) echo "无效，请重新输入" ;;
        esac
    done
}

do_uninstall() {
    step '1/3 检查卸载范围'
    acquire_lock
    if [ "$KEEP_INSTANCES" = "1" ]; then
        log "只卸 Agent，保留实例与数据"
    else
        confirm_purge
        step '2/3 删除 particeps 专用资源'
        task_state '已开始删除 particeps 项目资源，可能部分完成；不会自动恢复已删除资源'
        delete_particeps_instances
        delete_particeps_resources
    fi
    step '3/3 停止并移除 Agent'
    if [ "$KEEP_INSTANCES" = "1" ]; then
        task_state '正在停止并移除 Agent；实例、配置和数据保留'
    fi
    if command -v systemctl >/dev/null 2>&1; then
        if [ -f "$AGENT_UNIT_FILE" ] || systemctl is-active --quiet "$AGENT_SERVICE"; then
            systemctl stop "$AGENT_SERVICE" || die "无法停止 Agent，未移除程序与数据"
            if systemctl is-active --quiet "$AGENT_SERVICE"; then
                die "Agent 仍在运行，未移除程序与数据"
            fi
        fi
        systemctl disable "$AGENT_SERVICE" 2>/dev/null || true
    fi
    rm -f "$AGENT_UNIT_FILE"
    rm -rf "$AGENT_BIN_DIR"
    if command -v systemctl >/dev/null 2>&1; then
        systemctl daemon-reload
    fi
    if [ "$KEEP_INSTANCES" = "1" ]; then
        task_state "Agent 已移除；实例、配置和数据保留在 ${AGENT_CONFIG_DIR} 与 ${AGENT_DATA_DIR}"
        log "已移除程序与服务；配置与数据保留在 ${AGENT_CONFIG_DIR} 和 ${AGENT_DATA_DIR}"
        return
    fi
    rm -rf "$AGENT_CONFIG_DIR" "$AGENT_DATA_DIR"
    task_state '已全部卸载 particeps 受管资源，保留 Incus'
    log "已全部卸载 particeps 受管资源"
    prompt_remove_incus
}

do_update() {
    step '1/5 检查升级条件'
    preflight_existing
    acquire_lock
    local staged="$TASK_WORK_DIR/agent" was_active=0
    if systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
        was_active=1
    fi
    step '2/5 下载并校验 Release'
    download_agent "$staged"
    step '3/5 备份现有安装'
    task_state '正在创建升级备份；现有程序、配置与服务尚未替换' 1
    backup_existing
    step '4/5 替换 Agent 程序'
    task_state "已开始替换 Agent；升级前备份: $UPGRADE_BACKUP"
    mv "$staged" "$AGENT_BINARY"
    chmod 0755 "$AGENT_BINARY"
    step '5/5 核对服务状态'
    if [ "$was_active" = "1" ]; then
        systemctl restart "$AGENT_SERVICE" || die "Agent 重启失败。备份: $UPGRADE_BACKUP"
        verify_active_service
        task_state "Agent 已更新且服务运行正常；升级前备份: $UPGRADE_BACKUP"
    else
        log "Agent 原本已停止，本次保持停止"
        task_state "Agent 已更新并保持停止；升级前备份: $UPGRADE_BACKUP"
    fi
    log "仅 Agent 更新完成；实例、网桥、存储池和配置值未改动"
}

do_fresh() {
    step '1/6 检查安装条件'
    [ "$UPDATE_ONLY" = "0" ] || die "未找到标准安装；--update-only 不会执行首次安装"
    acquire_lock
    if [ "$SKIP_HOST" != "1" ]; then
        host_preflight
        if ! command -v incus >/dev/null 2>&1 || ! incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
            prompt_pool_size
        fi
        step '2/6 准备系统依赖'
        task_state '系统依赖可能已安装或部分更新；尚未写入 Agent'
        install_packages
    fi
    local staged="$TASK_WORK_DIR/agent"
    step '3/6 下载并校验 Release'
    download_agent "$staged"
    step '4/6 准备 Incus 资源'
    if [ "$SKIP_HOST" != "1" ]; then
        task_state '已安装依赖，Incus 专用资源可能部分创建；尚未写入 Agent'
        ensure_incus_resources
        setup_cgroup
    fi
    step '5/6 写入 Agent 与配置'
    task_state '正在写入 Agent、配置和管理员；安装可能部分完成'
    mkdir -p "$AGENT_BIN_DIR" "$AGENT_DATA_DIR/backups"
    chmod 0755 "$AGENT_BIN_DIR"
    chmod 0700 "$AGENT_DATA_DIR" "$AGENT_DATA_DIR/backups"
    mv "$staged" "$AGENT_BINARY"
    chmod 0755 "$AGENT_BINARY"
    if [ ! -f "$AGENT_CONFIG_FILE" ]; then
        write_default_config
    else
        log "已有配置 ${AGENT_CONFIG_FILE}，不覆盖"
    fi
    write_unit
    init_admin
    if command -v systemctl >/dev/null 2>&1; then
        systemctl daemon-reload
        systemctl enable --now "$AGENT_SERVICE"
        step '6/6 核对服务状态'
        verify_active_service
    fi
    task_state 'Agent 安装完成，服务运行正常'
    print_finish "$INSTALL_ADMIN_PASSWORD" "$INSTALL_ADMIN_EXISTING"
}

verify_active_service() {
    sleep 1
    systemctl is-active --quiet "$AGENT_SERVICE" || die "Agent 未能保持运行${UPGRADE_BACKUP:+。备份: $UPGRADE_BACKUP}"
}

execute_action() {
    TASK_STEP="准备操作"
    TASK_STATE="尚未修改程序、服务或 Incus 资源"
    TASK_ERROR=""
    TASK_RETRY=1
    TASK_WORK_DIR=""
    TASK_LOG=""
    TASK_LOG_PID=""
    UPGRADE_BACKUP=""
    trap 'finish_task "$?"' EXIT
    trap 'unexpected_error "$?" "$LINENO"' ERR
    trap 'cancel_task 130' INT
    trap 'cancel_task 143' TERM
    require_root
    mkdir -p "$LOG_DIR" "$(dirname "$LOCK_FILE")"
    chmod 0700 "$LOG_DIR"
    TASK_LOG=$(mktemp "$LOG_DIR/$(date +%Y%m%d-%H%M%S)-${ACTION}-XXXXXX")
    chmod 0600 "$TASK_LOG"
    # Record diagnostics only. stdout contains the one-time administrator password.
    exec 8>&2
    exec 2> >(trap '' INT TERM; exec tee -a "$TASK_LOG" >&8)
    TASK_LOG_PID=$!
    TASK_WORK_DIR=$(mktemp -d)
    case "$ACTION" in
        status)
            step '1/1 查询实际状态'
            show_status
            ;;
        rollback) do_rollback ;;
        uninstall) do_uninstall ;;
        install)
            if installed; then
                [ "$SKIP_HOST" = "1" ] || host_preflight
                do_update
            else
                do_fresh
            fi
            ;;
        *) die "未知动作: $ACTION" ;;
    esac
}

if [ -z "$ACTION" ] && [ "$ORIGINAL_ARGC" -eq 0 ] && [ -t 0 ] && [ "$NON_INTERACTIVE" != "1" ]; then
    menu_loop
else
    [ -t 0 ] || NON_INTERACTIVE=1
    [ -n "$ACTION" ] || ACTION="install"
    execute_action
fi
