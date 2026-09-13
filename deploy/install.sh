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
TASK_STATE=""
TASK_ERROR=""
TASK_RETRY=1
TASK_WORK_DIR=""
TASK_RESULT_DIR=""
TASK_LOG=""
TASK_LOG_PID=""
MENU_SESSION_DIR=""
MENU_EOF=0
RESULT_RETRY=0
RESULT_EDIT=0
LAST_TASK_CODE=0
SOFT_INTERRUPT=0
INTERRUPT_POLICY="static"
INTERRUPT_NOTICE=0
ROLLBACK_IN_PROGRESS=0
INSTALL_KIND=""
RESULT_KIND=""
AGENT_WAS_ACTIVE=0
AGENT_REPLACED=0
CREATED_POOL=0
CREATED_NETWORK=0
CREATED_PROJECT=0
CREATED_CONFIG=0
CREATED_DATA=0
WROTE_ADMIN=0
NETWORK_V6_PREV=""
NETWORK_V6_CHANGED=0
PRE_OP_DIR=""
TASK_DONE=""
TASK_UNCONFIRMED=""
TASK_MATERIALS=""
PURGE_INCUS_RUNNING=0
EXISTED_BINARY=0
EXISTED_UNIT=0

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >&2; }
die() { TASK_ERROR="$*"; RESULT_KIND="failed"; log "$TASK_ERROR"; exit 1; }
progress() {
    TASK_STEP="$1"
    printf '%s\n' "$1"
    log "$1"
}

task_state() {
    TASK_STATE="$1"
    TASK_RETRY="${2:-0}"
}

run_protected() {
    local st=0
    (
        trap '' INT TERM
        "$@"
    ) || st=$?
    return "$st"
}

unexpected_error() {
    local code="$1" line="$2"
    log "命令执行失败（退出码 ${code}，脚本第 ${line} 行）"
    [ -n "$TASK_ERROR" ] || TASK_ERROR="${TASK_STEP}失败"
}

write_result_files() {
    [ -n "$TASK_RESULT_DIR" ] || return 0
    printf '%s\n' "$TASK_STEP" > "$TASK_RESULT_DIR/step"
    printf '%s\n' "$TASK_STATE" > "$TASK_RESULT_DIR/state"
    printf '%s\n' "$TASK_ERROR" > "$TASK_RESULT_DIR/error"
    printf '%s\n' "$TASK_RETRY" > "$TASK_RESULT_DIR/retry"
    printf '%s\n' "$TASK_LOG" > "$TASK_RESULT_DIR/log"
    printf '%s\n' "${RESULT_KIND:-failed}" > "$TASK_RESULT_DIR/kind"
    printf '%s\n' "$TASK_DONE" > "$TASK_RESULT_DIR/done"
    printf '%s\n' "$TASK_UNCONFIRMED" > "$TASK_RESULT_DIR/unconfirmed"
    printf '%s\n' "$TASK_MATERIALS" > "$TASK_RESULT_DIR/materials"
    printf '%s\n' "$UPGRADE_BACKUP" > "$TASK_RESULT_DIR/backup"
}

print_failure_page() {
    local step error done unconfirmed materials logfile backup
    step="${1:-$TASK_STEP}"
    error="${2:-$TASK_ERROR}"
    done="${3:-$TASK_DONE}"
    unconfirmed="${4:-$TASK_UNCONFIRMED}"
    materials="${5:-$TASK_MATERIALS}"
    logfile="${6:-$TASK_LOG}"
    backup="${7:-$UPGRADE_BACKUP}"
    step="${step%…}"
    printf '\n%s失败。\n\n' "$step"
    [ -z "$error" ] || printf '原因：%s\n' "$error"
    [ -z "$done" ] || printf '已完成：%s\n' "$done"
    [ -z "$unconfirmed" ] || printf '未确认：%s\n' "$unconfirmed"
    [ -z "$backup" ] || printf '备份：%s\n' "$backup"
    [ -z "$materials" ] || printf '恢复材料：%s\n' "$materials"
    [ -z "$logfile" ] || printf '日志：%s\n' "$logfile"
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
        log "${TASK_ERROR:-未完成（退出码 $code）}；阶段: $TASK_STEP"
        [ -z "$TASK_STATE" ] || log "已完成: $TASK_STATE"
    fi
    write_result_files
    if [ -z "$MENU_SESSION_DIR" ]; then
        if [ "$code" -ne 0 ] && [ "$code" -ne 130 ] && [ "$code" -ne 131 ]; then
            print_failure_page
        fi
    fi
    exec 2>&8 8>&- 2>/dev/null || true
    if [ -n "$TASK_LOG_PID" ]; then
        wait "$TASK_LOG_PID" || true
    fi
    exit "$code"
}

require_root() {
    [ "$(id -u)" = "0" ] || die "请使用 root 运行脚本。"
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
particeps 安装脚本

用法：
  bash install.sh
      打开菜单

  bash install.sh --tag {版本}
      安装或更新到指定版本，默认 latest

  bash install.sh --update-only
      仅更新已有 Agent

  bash install.sh --status
      查看服务、监听地址和最近备份

  bash install.sh --rollback
      恢复最近的升级备份

  bash install.sh --uninstall
      全部卸载 particeps，交互时输入 PURGE 确认

  bash install.sh --uninstall --keep-instances
      仅卸载 Agent，保留实例、配置和管理数据

  bash install.sh --uninstall -y --confirm PURGE
      非交互全部卸载 particeps，保留 Incus

  bash install.sh -y
      非交互安装或更新，新存储池使用建议大小

  bash install.sh --help
      显示帮助

静态页面 Ctrl+C 退出；可取消任务执行中 Ctrl+C 软中断。
全部卸载和 Incus 清除开始后无法用 Ctrl+C 取消。

日志目录：/var/log/particeps-install/
环境变量：PARTICEPS_RELEASE_TAG、PARTICEPS_GITHUB_REPO
EOF
}

show_menu() {
    printf '\nparticeps\n\n'
    printf '  1) 安装或更新\n'
    printf '  2) 查看状态\n'
    printf '  3) 恢复备份\n'
    printf '  4) 全部卸载（删除实例和数据）\n'
    printf '  5) 仅卸载 Agent\n'
    printf '  0) 退出\n\n'
}

exit_script() {
    exit 130
}

menu_read() {
    trap 'exit_script' INT
    if read -r -p "$1" "$2"; then
        return 0
    fi
    MENU_EOF=1
    return 1
}

edit_release_tag() {
    local tag
    while true; do
        menu_read "版本 [${RELEASE_TAG}]（0 返回）：" tag || return 1
        [ "$tag" != "0" ] || return 1
        tag="${tag:-$RELEASE_TAG}"
        if [[ "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
            RELEASE_TAG="$tag"
            return 0
        fi
        printf '版本只能包含字母、数字、点、下划线和短横线。\n例如：latest、v0.1.1。\n'
    done
}

release_label() {
    if [ "$RELEASE_TAG" = "latest" ]; then
        printf 'latest（最新版本）'
    else
        printf '%s' "$RELEASE_TAG"
    fi
}

view_task_log() {
    local logfile="$1" choice
    printf '\n日志：%s\n\n' "${logfile:-无}"
    if [ -n "$logfile" ] && [ -f "$logfile" ]; then
        cat -- "$logfile" || printf '无法读取日志：文件无法打开。\n'
    elif [ -z "$logfile" ]; then
        printf '没有可用的日志。\n'
    else
        printf '无法读取日志：文件不存在。\n'
    fi
    printf '\n回车返回结果页：'
    menu_read '' choice
}

wait_result_input() {
    local code="$1" retry="$2" logfile="$3" choice allow_retry=0 allow_edit=0
    RESULT_RETRY=0
    RESULT_EDIT=0
    if [ "$code" -ne 0 ] && [ "$retry" = "1" ] && [ "$ACTION" != "uninstall" ] && [ "$ACTION" != "purge-incus" ]; then
        allow_retry=1
        [ "$ACTION" != "install" ] || allow_edit=1
    fi
    while true; do
        if [ "$allow_retry" = "1" ] && [ "$allow_edit" = "1" ]; then
            printf '\n回车返回菜单，R 重试，E 修改版本，L 查看日志：'
        elif [ "$allow_retry" = "1" ]; then
            printf '\n回车返回菜单，R 重试，L 查看日志：'
        else
            printf '\n回车返回菜单，L 查看日志：'
        fi
        menu_read '' choice || return 0
        case "$choice" in
            ''|0) return 0 ;;
            L|l)
                view_task_log "$logfile" || return 0
                ;;
            R|r)
                if [ "$allow_retry" = "1" ]; then
                    RESULT_RETRY=1
                    return 0
                fi
                printf '该选项当前不可用，请重新选择。\n'
                ;;
            E|e)
                if [ "$allow_edit" = "1" ]; then
                    edit_release_tag || continue
                    RESULT_EDIT=1
                    RESULT_RETRY=1
                    return 0
                fi
                printf '该选项当前不可用，请重新选择。\n'
                ;;
            *) printf '该选项当前不可用，请重新选择。\n' ;;
        esac
    done
}

show_result() {
    local code="$1" kind logfile retry
    RESULT_RETRY=0
    kind=$(cat "$TASK_RESULT_DIR/kind" 2>/dev/null || true)
    logfile=$(cat "$TASK_RESULT_DIR/log" 2>/dev/null || true)
    retry=$(cat "$TASK_RESULT_DIR/retry" 2>/dev/null || printf 0)
    if [ "$code" -eq 0 ]; then
        wait_result_input "$code" "$retry" "$logfile"
        return
    fi
    if [ "$code" -eq 130 ] && [ "$kind" != "rollback_failed" ]; then
        return 0
    fi
    if [ "$code" -eq 131 ] || [ "$kind" = "rollback_failed" ]; then
        wait_result_input "$code" 0 "$logfile"
        return
    fi
    print_failure_page \
        "$(cat "$TASK_RESULT_DIR/step" 2>/dev/null || printf 操作)" \
        "$(cat "$TASK_RESULT_DIR/error" 2>/dev/null || true)" \
        "$(cat "$TASK_RESULT_DIR/done" 2>/dev/null || true)" \
        "$(cat "$TASK_RESULT_DIR/unconfirmed" 2>/dev/null || true)" \
        "$(cat "$TASK_RESULT_DIR/materials" 2>/dev/null || true)" \
        "$logfile" \
        "$(cat "$TASK_RESULT_DIR/backup" 2>/dev/null || true)"
    wait_result_input "$code" "$retry" "$logfile"
}

run_task() {
    TASK_RESULT_DIR=$(mktemp -d "$MENU_SESSION_DIR/result-XXXXXX")
    # The task handles terminal SIGINT; keep its waiting menu alive.
    # Use a caught signal so child commands do not inherit SIGINT as ignored.
    trap ':' INT
    set +e
    (
        set -Eeuo pipefail
        execute_action
    )
    LAST_TASK_CODE=$?
    trap 'exit_script' INT
    set -e
}

confirm_update() {
    local choice agent
    agent=$(agent_state_label)
    while true; do
        printf '\n更新 Agent\n\n'
        printf '目标版本：%s\n' "$(release_label)"
        printf '更新前会备份程序、服务文件、配置和管理数据。\n'
        printf '实例、网络和存储池保持不变。\n'
        printf 'Agent 当前状态：%s\n' "$agent"
        if [ "$agent" = "运行中" ]; then
            printf '\n更新期间面板会短暂不可用，实例继续运行。\n'
        elif [ "$agent" = "已停止" ]; then
            printf '\n更新后保持停止。\n'
        fi
        printf '\n'
        menu_read '回车开始更新，E 修改版本，0 返回：' choice || return 1
        case "$choice" in
            '') return 0 ;;
            0) return 1 ;;
            E|e) edit_release_tag || true ;;
            *) printf '该选项当前不可用，请重新选择。\n' ;;
        esac
    done
}

confirm_fresh_install() {
    local choice pool_line net_line v6_line cfg_line listen_line
    if [ "$SKIP_HOST" != "1" ]; then
        if ! command -v incus >/dev/null 2>&1 || ! incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
            prompt_pool_size || return 1
            pool_line="新建 particeps-pool，大小为 ${POOL_SIZE%GiB} GiB"
        else
            pool_line="使用现有存储池"
        fi
        if command -v incus >/dev/null 2>&1 && incus network show "$INCUS_NETWORK" >/dev/null 2>&1; then
            net_line="使用现有网桥"
            v6_line="保持现有配置"
        else
            net_line="新建 particepsbr0"
            if host_has_ipv6; then
                v6_line="将启用网桥 IPv6 NAT"
            else
                v6_line="不启用"
            fi
        fi
    else
        pool_line="使用现有存储池"
        net_line="使用现有网桥"
        v6_line="保持现有配置"
    fi
    if [ -f "$AGENT_CONFIG_FILE" ]; then
        cfg_line="使用现有配置"
        listen_line=$(listen_addr)
        [ -n "$listen_line" ] || listen_line="无法读取"
    else
        cfg_line="新建配置"
        listen_line="0.0.0.0:8792"
    fi
    while true; do
        printf '\n安装 particeps\n\n'
        printf '版本：%s\n' "$(release_label)"
        printf '存储池：%s\n' "$pool_line"
        printf '网络：%s\n' "$net_line"
        printf 'IPv6：%s\n' "$v6_line"
        printf '配置：%s\n' "$cfg_line"
        printf '监听地址：%s\n\n' "$listen_line"
        menu_read '回车开始安装，E 修改版本，0 返回：' choice || return 1
        case "$choice" in
            '') return 0 ;;
            0) return 1 ;;
            E|e) edit_release_tag || true ;;
            *) printf '该选项当前不可用，请重新选择。\n' ;;
        esac
    done
}

confirm_install() {
    local missing
    missing=$(missing_install_parts)
    if [ -n "$missing" ] && ! installed; then
        printf '\n检测到不完整的安装，缺少：%s。\n请先处理现有安装。\n' "$missing"
        printf '\n回车返回菜单：'
        menu_read '' _
        return 1
    fi
    if installed; then
        confirm_update
    else
        confirm_fresh_install
    fi
}

confirm_rollback() {
    local choice target
    target=$(cat "$BACKUP_ROOT/latest" 2>/dev/null || true)
    if [ -z "$target" ] || [ ! -d "$target" ]; then
        printf '\n未找到可用的升级备份。\n'
        printf '\n回车返回菜单：'
        menu_read '' _
        return 1
    fi
    if [ ! -f "$target/particeps-agent" ] || [ ! -f "$target/config.yaml" ] || [ ! -f "$target/particeps-agent.service" ] || [ ! -f "$target/state.db" ]; then
        printf '\n备份不完整，缺少：程序、配置、服务文件或管理数据。\n备份：%s\n' "$target"
        printf '\n回车返回菜单，L 查看日志：'
        menu_read '' _
        return 1
    fi
    while true; do
        printf '\n恢复备份\n\n'
        printf '备份：%s\n' "$target"
        printf '恢复内容：程序、服务文件、配置和管理数据。\n\n'
        printf '备份之后的管理数据会被覆盖，实例磁盘不会随之恢复。\n\n'
        menu_read '回车开始恢复，0 返回：' choice || return 1
        case "$choice" in
            '') return 0 ;;
            0) return 1 ;;
            *) printf '该选项当前不可用，请重新选择。\n' ;;
        esac
    done
}

confirm_keep_uninstall() {
    local choice
    while true; do
        printf '\n仅卸载 Agent\n\n'
        printf '将移除 Agent 程序和服务。\n'
        printf '实例、网络、存储池、配置和管理数据保留。\n\n'
        menu_read '回车开始卸载，0 返回：' choice || return 1
        case "$choice" in
            '') return 0 ;;
            0) return 1 ;;
            *) printf '该选项当前不可用，请重新选择。\n' ;;
        esac
    done
}

list_particeps_instances() {
    local names
    names=$(instance_names "$INCUS_PROJECT" 2>/dev/null || true)
    if [ -z "$names" ]; then
        printf '  无\n'
        return
    fi
    while IFS= read -r name; do
        [ -n "$name" ] || continue
        printf '  %s\n' "$name"
    done <<< "$names"
}

confirm_full_uninstall() {
    local answer
    printf '\n全部卸载 particeps\n\n'
    printf '将删除：\n'
    printf '  particeps 项目中的全部实例\n'
    printf '  网桥 particepsbr0\n'
    printf '  存储池 particeps-pool\n'
    printf '  particeps 项目\n'
    printf '  Agent 程序和服务\n'
    printf '  配置、管理数据和本机升级备份\n\n'
    printf '实例：\n'
    list_particeps_instances
    printf '\n其他项目和 Incus 在这一步保留。\n'
    printf '开始后无法用 Ctrl+C 取消。\n\n'
    menu_read '输入 PURGE 确认，其他输入返回：' answer || return 1
    [ "$answer" = "PURGE" ]
}

print_foreign_instances() {
    local proj names line name status found=0 projects
    command -v incus >/dev/null 2>&1 || return 1
    if ! projects=$(incus project list --format csv -c n 2>/dev/null); then
        printf '其他项目中的实例：查询失败。\n'
        return 2
    fi
    while IFS= read -r proj; do
        proj="${proj%%,*}"
        proj="${proj//$'\r'/}"
        [ -n "$proj" ] || continue
        [ "$proj" = "$INCUS_PROJECT" ] && continue
        if ! names=$(incus --project "$proj" list --format csv -c n,s 2>/dev/null); then
            printf '其他项目中的实例：查询失败。\n'
            return 2
        fi
        while IFS= read -r line; do
            [ -n "$line" ] || continue
            name="${line%%,*}"
            status="${line#*,}"
            [ -n "$name" ] || continue
            if [ "$found" = "0" ]; then
                printf '其他项目中的实例：\n'
                found=1
            fi
            printf '  %s / %s / %s\n' "$proj" "$name" "$status"
        done <<< "$names"
    done <<< "$projects"
    if [ "$found" = "0" ]; then
        printf '其他项目中没有实例。\n'
        return 1
    fi
    return 0
}

followup_incus_prompt() {
    local answer code logfile list_rc
    logfile=$(cat "$TASK_RESULT_DIR/log" 2>/dev/null || true)
    if ! command -v incus >/dev/null 2>&1; then
        printf '\nparticeps 已卸载，未检测到 Incus。\n'
        printf '\n回车返回菜单，L 查看日志：'
        menu_read '' _ || true
        return 0
    fi
    printf '\nparticeps 已卸载，Incus 仍保留。\n\n'
    if print_foreign_instances; then
        list_rc=0
        printf '\n清除 Incus 也会删除以上实例及 Incus 数据。\n'
    else
        list_rc=$?
        if [ "$list_rc" -eq 2 ]; then
            printf '\n清除 Incus 也会删除其他项目中的实例及 Incus 数据。\n'
        fi
    fi
    printf '开始后无法用 Ctrl+C 取消。\n\n'
    while true; do
        menu_read '卸载 Incus 并清除全部数据？[y/N]：' answer || return 0
        case "$answer" in
            Y|y)
                ACTION="purge-incus"
                run_task
                show_result "$LAST_TASK_CODE"
                ACTION="uninstall"
                return 0
                ;;
            N|n|"")
                printf '\n已保留 Incus。\n'
                printf '\n回车返回菜单，L 查看日志：'
                menu_read '' _ || true
                return 0
                ;;
            *) printf '请输入 y 或 n。\n' ;;
        esac
    done
}

prepare_action() {
    case "$ACTION" in
        status) return 0 ;;
        install) confirm_install ;;
        rollback) confirm_rollback ;;
        uninstall)
            if [ "$KEEP_INSTANCES" = "1" ]; then
                confirm_keep_uninstall
            else
                confirm_full_uninstall
            fi
            ;;
        *) return 0 ;;
    esac
}

status_menu() {
    local choice logfile
    while [ "$MENU_EOF" = "0" ]; do
        ACTION="status"
        run_task
        if [ "$LAST_TASK_CODE" -ne 0 ]; then
            show_result "$LAST_TASK_CODE"
            rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
            [ "$RESULT_RETRY" = "1" ] || return 0
            continue
        fi
        logfile=$(cat "$TASK_RESULT_DIR/log" 2>/dev/null || true)
        rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
        while [ "$MENU_EOF" = "0" ]; do
            printf '\n  1) 启动 Incus\n'
            printf '  2) 停止 Incus\n'
            printf '  3) 启动 Agent\n'
            printf '  4) 停止 Agent\n'
            printf '  R) 刷新状态\n'
            printf '  0) 返回主菜单\n\n'
            printf '启动 Agent 会按服务依赖启动 Incus。停止 Incus 期间实例管理不可用。\n'
            menu_read '请选择操作，回车返回菜单，L 查看日志：' choice || return 0
            case "$choice" in
                ''|0) return 0 ;;
                R|r) break ;;
                L|l) view_task_log "$logfile" || return 0 ;;
                1) ACTION="start-incus"; break ;;
                2) ACTION="stop-incus"; break ;;
                3) ACTION="start-agent"; break ;;
                4) ACTION="stop-agent"; break ;;
                *) printf '请输入 1 到 4、R、L，或回车/0 返回。\n' ;;
            esac
        done
        [ "$ACTION" != "status" ] || continue
        while [ "$MENU_EOF" = "0" ]; do
            run_task
            show_result "$LAST_TASK_CODE"
            rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
            [ "$RESULT_RETRY" = "1" ] || break
        done
    done
}

menu_loop() {
    local choice code
    MENU_SESSION_DIR=$(mktemp -d)
    trap 'rm -rf -- "$MENU_SESSION_DIR"' EXIT
    trap 'exit_script' INT
    while [ "$MENU_EOF" = "0" ]; do
        ACTION=""
        UPDATE_ONLY=0
        KEEP_INSTANCES=0
        CONFIRM_WORD=""
        show_menu
        if ! menu_read '请选择 [0-5]：' choice; then
            return 0
        fi
        case "$choice" in
            1) ACTION="install" ;;
            2) status_menu; continue ;;
            3) ACTION="rollback" ;;
            4) ACTION="uninstall" ;;
            5) ACTION="uninstall"; KEEP_INSTANCES=1 ;;
            0) return 0 ;;
            *) printf '请输入 0 到 5。\n'; continue ;;
        esac
        if ! prepare_action; then
            continue
        fi
        while true; do
            run_task
            if [ "$LAST_TASK_CODE" -eq 0 ] && [ "$ACTION" = "uninstall" ] && [ "$KEEP_INSTANCES" != "1" ] && [ -f "$TASK_RESULT_DIR/incus_prompt" ]; then
                followup_incus_prompt
                rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
                break
            fi
            show_result "$LAST_TASK_CODE"
            rm -rf -- "$TASK_RESULT_DIR" || log "结果临时目录清理失败: $TASK_RESULT_DIR"
            [ "$RESULT_RETRY" = "1" ] || break
            if [ "$RESULT_EDIT" = "1" ]; then
                prepare_action || break
            fi
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
        --tag) [ $# -ge 2 ] || die "参数 --tag 缺少值。"; RELEASE_TAG="$2"; shift 2 ;;
        --confirm) [ $# -ge 2 ] || die "参数 --confirm 缺少值。"; CONFIRM_WORD="$2"; shift 2 ;;
        -y|--yes|--non-interactive) NON_INTERACTIVE=1; shift ;;
        -h|--help) show_help; exit 0 ;;
        *) die "未知参数：$1" ;;
    esac
done

acquire_lock() {
    exec 9>"$LOCK_FILE"
    flock -n 9 || die "已有安装操作正在运行，请稍后再试。"
}

host_preflight() {
    if [ "$(uname -m)" != "x86_64" ] || [ ! -f /etc/os-release ] || ! grep -q 'VERSION_ID="13"' /etc/os-release; then
        die "当前系统不受支持，需要 Debian 13 amd64。"
    fi
}

installed() {
    [ -f "$AGENT_BINARY" ] && [ -f "$AGENT_CONFIG_FILE" ] && [ -f "$AGENT_UNIT_FILE" ]
}

missing_install_parts() {
    local parts=()
    [ -f "$AGENT_BINARY" ] || parts+=("Agent 程序")
    [ -f "$AGENT_CONFIG_FILE" ] || parts+=("配置文件")
    [ -f "$AGENT_UNIT_FILE" ] || parts+=("服务文件")
    if [ "${#parts[@]}" -eq 0 ] || [ "${#parts[@]}" -eq 3 ]; then
        return 0
    fi
    local IFS='、'
    printf '%s' "${parts[*]}"
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
    local avail default size
    avail=$(root_avail_g)
    if [ "${avail:-0}" -lt 1 ]; then
        TASK_ERROR="根分区可用空间不足 1 GiB，无法创建存储池。"
        if [ -n "$TASK_WORK_DIR" ]; then
            die "$TASK_ERROR"
        fi
        printf '%s\n' "$TASK_ERROR"
        return 1
    fi
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
    printf '\n根分区可用：%s GiB\n' "$avail"
    printf '建议存储池大小：%s GiB\n\n' "$default"
    while true; do
        size=""
        menu_read "存储池大小（GiB）[${default}]（0 返回）：" size || return 1
        [ "$size" != "0" ] || return 1
        if [ -z "$size" ]; then
            size="$default"
        elif ! [[ "$size" =~ ^[1-9][0-9]*$ ]] || [ "${#size}" -gt "${#avail}" ] || [ "$size" -gt "$avail" ]; then
            printf '请输入 1 到 %s 的整数。\n' "$avail"
            continue
        fi
        POOL_SIZE="${size}GiB"
        if [ -n "$MENU_SESSION_DIR" ]; then
            printf '%s\n' "$size" > "$MENU_SESSION_DIR/pool-size"
        fi
        return 0
    done
}

sqlite_backup() {
    local src="$1" dest="$2"
    (
        trap '' INT TERM
        python3 - "$src" "$dest" <<'PY'
import pathlib, sqlite3, sys
src, dest = sys.argv[1], sys.argv[2]
with sqlite3.connect(pathlib.Path(src).as_uri() + "?mode=ro", uri=True, timeout=30) as source:
    with sqlite3.connect(dest) as target:
        source.backup(target)
        if target.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
            raise SystemExit("database backup failed integrity check")
PY
    )
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

systemd_label() {
    local svc="$1" st load_state
    if ! command -v systemctl >/dev/null 2>&1 || ! load_state=$(systemctl show --property=LoadState --value "$svc" 2>/dev/null); then
        printf '查询失败'
        return
    fi
    case "$load_state" in
        not-found) printf '未安装'; return ;;
        loaded|masked) ;;
        *) printf '查询失败'; return ;;
    esac
    st=$(systemctl is-active "$svc" 2>/dev/null || true)
    case "$st" in
        active) printf '运行中' ;;
        inactive) printf '已停止' ;;
        failed) printf '异常' ;;
        activating) printf '启动中' ;;
        deactivating) printf '停止中' ;;
        *) printf '查询失败' ;;
    esac
}

agent_state_label() {
    if [ ! -f "$AGENT_BINARY" ] && [ ! -f "$AGENT_UNIT_FILE" ]; then
        printf '未安装'
        return
    fi
    systemd_label "$AGENT_SERVICE"
}

incus_state_label() {
    systemd_label incus.service
}

control_service() {
    local label="$1" service="$2" operation="$3" verb result expected load_state unit state
    local -a units=("$service")
    if [ "$operation" = "start" ]; then
        verb="启动"; result="已启动"; expected="active"
    else
        verb="停止"; result="已停止"; expected="inactive"
    fi
    progress "检查 ${label} 服务…"
    acquire_lock
    command -v systemctl >/dev/null 2>&1 || die "无法控制 ${label}：未找到 systemctl。"
    load_state=$(systemctl show --property=LoadState --value "$service") || die "无法查询 ${label} 服务。"
    case "$load_state" in
        loaded|masked) ;;
        not-found) die "${label} 未安装，请先安装后再操作。" ;;
        *) die "${label} 服务不可用（${load_state:-状态未知}）。" ;;
    esac
    if [ "$service" = "incus.service" ]; then
        load_state=$(systemctl show --property=LoadState --value incus.socket) || die "无法查询 Incus socket。"
        case "$load_state" in
            loaded|masked) units+=(incus.socket) ;;
            not-found) ;;
            *) die "Incus socket 不可用（${load_state:-状态未知}）。" ;;
        esac
    fi
    progress "${verb} ${label}…"
    # Include the socket in the same transaction so requests cannot reactivate a stopped Incus.
    run_protected systemctl "$operation" "${units[@]}" || die "${verb} ${label} 失败：服务操作未完成。"
    task_state "已执行 ${label} ${verb}请求" 1
    if [ "$operation" = "start" ]; then
        run_protected sleep 1
    fi
    progress "检查 ${label} 状态…"
    for unit in "${units[@]}"; do
        state=$(run_protected systemctl is-active "$unit" 2>/dev/null || true)
        if [ "$state" != "$expected" ]; then
            TASK_UNCONFIRMED="${label} 的${verb}结果"
            die "${label} 状态未确认：${unit} 当前为 ${state:-查询失败}。"
        fi
    done
    RESULT_KIND="success"
    printf '\n%s %s。\n' "$label" "$result"
    # A service job already submitted to systemd must finish before returning to the menu.
    [ "$SOFT_INTERRUPT" != "1" ] || exit 130
}

show_status() {
    local agent_state="inactive" incus_state="n/a" backup="none" listen="n/a" sha="n/a"
    if command -v systemctl >/dev/null 2>&1; then
        agent_state=$(systemctl is-active "$AGENT_SERVICE" 2>/dev/null || true)
        incus_state=$(systemctl is-active incus.service 2>/dev/null || true)
    fi
    [ -f "$AGENT_CONFIG_FILE" ] && listen=$(listen_addr)
    [ -f "$AGENT_BINARY" ] && command -v sha256sum >/dev/null 2>&1 && sha=$(sha256sum "$AGENT_BINARY" | awk '{print $1}')
    if [ -f "$BACKUP_ROOT/latest" ]; then
        backup=$(cat "$BACKUP_ROOT/latest")
    fi
    check_interrupt
    if [ -n "$MENU_SESSION_DIR" ]; then
        local agent_l incus_l listen_l backup_l sha_l
        agent_l=$(agent_state_label)
        incus_l=$(incus_state_label)
        if [ -f "$AGENT_CONFIG_FILE" ]; then
            listen_l="${listen:-无法读取}"
        else
            listen_l="未配置"
        fi
        if [ -f "$BACKUP_ROOT/latest" ]; then
            backup_l="$backup"
        else
            backup_l="无"
        fi
        if [ -f "$AGENT_BINARY" ]; then
            sha_l="${sha:-无法读取}"
        else
            sha_l="未安装"
        fi
        printf '\nparticeps 状态\n\n'
        printf 'Agent：%s\n' "$agent_l"
        printf 'Incus：%s\n' "$incus_l"
        printf '监听地址：%s\n' "$listen_l"
        printf '最近备份：%s\n' "$backup_l"
        printf '程序 SHA256：%s\n' "$sha_l"
        return
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
    (
        trap '' INT TERM
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
    )
}

write_unit() {
    mkdir -p "$(dirname "$AGENT_UNIT_FILE")"
    (
        trap '' INT TERM
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
    )
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
    progress "下载 Agent…"
    if ! curl -fsSL -o "$tmp/$ASSET_NAME" "${base}/${ASSET_NAME}"; then
        rm -rf "$tmp"
        check_interrupt
        die "下载 Agent 失败：无法从 GitHub Release 获取文件。"
    fi
    check_interrupt
    if ! curl -fsSL -o "$tmp/$CHECKSUM_NAME" "${base}/${CHECKSUM_NAME}"; then
        rm -rf "$tmp"
        check_interrupt
        die "下载校验文件失败：无法获取 SHA256SUMS。"
    fi
    check_interrupt
    progress "校验安装文件…"
    if ! ( cd "$tmp" && grep -E "[[:space:]]${ASSET_NAME}\$" "$CHECKSUM_NAME" | sha256sum -c - >/dev/null ); then
        rm -rf "$tmp"
        die "文件校验失败，请重新下载。"
    fi
    if ! verify_elf64 "$tmp/$ASSET_NAME"; then
        rm -rf "$tmp"
        die "安装文件格式不正确，请重新下载。"
    fi
    mkdir -p "$(dirname "$dest")"
    cp "$tmp/$ASSET_NAME" "$dest"
    chmod 0755 "$dest"
    rm -rf "$tmp"
    check_interrupt
}

backup_existing() {
    local db metrics
    mkdir -p "$BACKUP_ROOT"
    chmod 0700 "$BACKUP_ROOT"
    UPGRADE_BACKUP=$(mktemp -d "$BACKUP_ROOT/upgrade-$(date +%Y%m%d-%H%M%S)-XXXXXX")
    chmod 0700 "$UPGRADE_BACKUP"
    run_protected cp -p "$AGENT_BINARY" "$UPGRADE_BACKUP/particeps-agent"
    run_protected cp -p "$AGENT_CONFIG_FILE" "$UPGRADE_BACKUP/config.yaml"
    run_protected cp -p "$AGENT_UNIT_FILE" "$UPGRADE_BACKUP/particeps-agent.service"
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
    local missing
    missing=$(missing_install_parts)
    if [ -n "$missing" ]; then
        die "检测到不完整的安装，缺少：${missing}。"
    fi
    installed || die "未检测到完整安装，无法执行更新。"
    command -v python3 >/dev/null || die "需要 python3 以备份管理库"
    command -v curl >/dev/null || die "需要 curl 以下载 Release"
    command -v sha256sum >/dev/null || die "需要 sha256sum"
    if [ "$SKIP_HOST" != "1" ]; then
        grep -q "$AGENT_BINARY" "$AGENT_UNIT_FILE" || die "非标准 ExecStart 路径，需手工处理"
        grep -q "$AGENT_CONFIG_FILE" "$AGENT_UNIT_FILE" || die "非标准配置路径，需手工处理"
    fi
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
    local port iface ipaddr found=0
    port=$(listen_port)
    command -v ip >/dev/null 2>&1 || return 1
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
    [ "$found" = "1" ]
}

print_finish() {
    local pass="$1" existing="$2"
    printf '\n安装完成。\n\n'
    printf '面板地址：\n'
    if ! print_panel_urls; then
        printf '未检测到可用的面板 IPv4 地址，请检查服务器网络。\n'
    fi
    printf '\n'
    if [ "$existing" = "1" ]; then
        printf '管理员密码沿用原设置。\n'
    else
        printf '初始管理员密码：%s\n' "$pass"
        printf '密码仅显示这一次，请保存。\n'
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
        if ! run_protected env PARTICEPS_ADMIN_PASSWORD="$INSTALL_ADMIN_PASSWORD" "$AGENT_BINARY" --config "$AGENT_CONFIG_FILE" --bootstrap-only; then
            check_interrupt
            die "写入管理员密码失败"
        fi
        WROTE_ADMIN=1
        check_interrupt
    fi
}

ensure_incus_resources() {
    if incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
        local driver
        driver=$(incus storage show "$INCUS_POOL" | awk '/^driver:/ {print $2; exit}')
        [ "$driver" = "lvm" ] || die "存储池 particeps-pool 使用 ${driver}，需要 lvm，无法继续安装。"
        printf '使用现有存储池。\n'
        log "存储池 ${INCUS_POOL} 已存在，不修改"
    else
        [ -n "$POOL_SIZE" ] || die "未设置存储池大小"
        progress "创建存储池…"
        run_protected incus storage create "$INCUS_POOL" lvm size="$POOL_SIZE" lvm.use_thinpool=true || die "创建存储池失败：incus 命令未成功。"
        CREATED_POOL=1
        check_interrupt
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
        [ "$addr" = "$EXPECTED_IPV4" ] || die "网桥 particepsbr0 的配置不符合要求。
当前 IPv4：${addr}
需要的 IPv4：${EXPECTED_IPV4}"
        [ "$nat" = "true" ] || die "网桥 particepsbr0 的配置不符合要求。
当前 IPv4：${addr}
需要的 IPv4：${EXPECTED_IPV4}"
        if [ "$want_v6" = "1" ] && { [ -z "$v6" ] || [ "$v6" = "none" ]; }; then
            NETWORK_V6_PREV="$v6"
            run_protected incus network set "$INCUS_NETWORK" ipv6.address=auto ipv6.nat=true || die "创建网桥失败：无法补配 IPv6。"
            NETWORK_V6_CHANGED=1
            check_interrupt
        fi
        printf '使用现有网桥。\n'
    else
        progress "配置网桥…"
        if [ "$want_v6" = "1" ]; then
            run_protected incus network create "$INCUS_NETWORK" ipv4.address="$EXPECTED_IPV4" ipv4.nat=true ipv6.address=auto ipv6.nat=true || die "创建网桥失败：incus 命令未成功。"
        else
            run_protected incus network create "$INCUS_NETWORK" ipv4.address="$EXPECTED_IPV4" ipv4.nat=true ipv6.address=none || die "创建网桥失败：incus 命令未成功。"
        fi
        CREATED_NETWORK=1
        check_interrupt
    fi

    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        log "项目 ${INCUS_PROJECT} 已存在，不修改"
    else
        progress "创建 particeps 项目…"
        run_protected incus project create "$INCUS_PROJECT" -c features.images=true -c features.profiles=true -c features.storage.volumes=true || die "创建 particeps 项目失败。"
        CREATED_PROJECT=1
        check_interrupt
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

snapshot_packages() {
    command -v dpkg-query >/dev/null 2>&1 || return 0
    dpkg-query -W -f '${Package}\n' 2>/dev/null | sort > "$TASK_WORK_DIR/packages-before" || true
}

remove_new_packages() {
    local list
    [ -f "$TASK_WORK_DIR/packages-before" ] || return 0
    command -v dpkg-query >/dev/null 2>&1 || return 0
    dpkg-query -W -f '${Package}\n' 2>/dev/null | sort > "$TASK_WORK_DIR/packages-after" || return 0
    list=$(comm -13 "$TASK_WORK_DIR/packages-before" "$TASK_WORK_DIR/packages-after" | tr '\n' ' ')
    [ -n "${list// }" ] || return 0
    export DEBIAN_FRONTEND=noninteractive
    # shellcheck disable=SC2086
    run_protected apt-get purge -y $list || return 1
}

install_packages() {
    export DEBIAN_FRONTEND=noninteractive
    snapshot_packages
    progress "安装依赖…"
    run_protected apt-get update -y || die "安装依赖失败：无法更新软件包列表。"
    check_interrupt
    run_protected apt-get install -y incus-base incus-client lvm2 thin-provisioning-tools uidmap conntrack python3 ca-certificates curl || die "安装依赖失败：无法安装所需软件包。"
    check_interrupt
    run_protected systemctl enable --now incus.service incus.socket 2>/dev/null || run_protected systemctl enable --now incus || true
    check_interrupt
}

on_interrupt() {
    case "$INTERRUPT_POLICY" in
        service)
            SOFT_INTERRUPT=1
            if [ "$INTERRUPT_NOTICE" != 1 ]; then
                INTERRUPT_NOTICE=1
                printf '\n正在变更服务状态，完成并核对后返回状态菜单。\n'
            fi
            ;;
        ignore)
            if [ "$INTERRUPT_NOTICE" != 1 ]; then
                INTERRUPT_NOTICE=1
                if [ "$PURGE_INCUS_RUNNING" = 1 ]; then
                    printf '\n正在清除 Incus，请等待完成。\n'
                else
                    printf '\n卸载已开始，请等待完成。\n'
                fi
            fi
            ;;
        query)
            SOFT_INTERRUPT=1
            ;;
        soft)
            [ "$SOFT_INTERRUPT" = 1 ] && return
            SOFT_INTERRUPT=1
            case "$ACTION:$INSTALL_KIND" in
                install:update) printf '\n正在取消更新，等待当前步骤结束…\n' ;;
                rollback:*) printf '\n正在取消恢复，等待当前步骤结束…\n' ;;
                uninstall:*) printf '\n正在取消卸载，等待当前步骤结束…\n' ;;
                *) printf '\n正在取消安装，等待当前步骤结束…\n' ;;
            esac
            ;;
        *)
            exit 130
            ;;
    esac
}

check_interrupt() {
    [ "$SOFT_INTERRUPT" = 1 ] || return 0
    [ "$ROLLBACK_IN_PROGRESS" = 1 ] && return 0
    if [ "$INTERRUPT_POLICY" = "query" ]; then
        RESULT_KIND="cancelled"
        printf '已停止查询。\n'
        exit 130
    fi
    perform_rollback_exit
}

save_agent_pre_op() {
    PRE_OP_DIR="$TASK_WORK_DIR/pre-op"
    mkdir -p "$PRE_OP_DIR"
    if [ -f "$AGENT_BINARY" ]; then
        cp -p "$AGENT_BINARY" "$PRE_OP_DIR/particeps-agent"
        EXISTED_BINARY=1
    fi
    if [ -f "$AGENT_UNIT_FILE" ]; then
        cp -p "$AGENT_UNIT_FILE" "$PRE_OP_DIR/particeps-agent.service"
        EXISTED_UNIT=1
    fi
    if [ -f "$AGENT_CONFIG_FILE" ]; then
        cp -p "$AGENT_CONFIG_FILE" "$PRE_OP_DIR/config.yaml"
    fi
    if [ -f "$(state_db_path)" ]; then
        cp -p "$(state_db_path)" "$PRE_OP_DIR/state.db" 2>/dev/null || true
    fi
    if [ -f "$(metrics_db_path)" ]; then
        cp -p "$(metrics_db_path)" "$PRE_OP_DIR/metrics.db" 2>/dev/null || true
    fi
}

restore_files_from() {
    local src="$1"
    [ -d "$src" ] || return 1
    mkdir -p "$AGENT_BIN_DIR" "$AGENT_CONFIG_DIR" "$(dirname "$AGENT_UNIT_FILE")" "$AGENT_DATA_DIR"
    if [ -f "$src/particeps-agent" ]; then
        run_protected cp -p "$src/particeps-agent" "$AGENT_BINARY" || return 1
        chmod 0755 "$AGENT_BINARY"
    fi
    if [ -f "$src/config.yaml" ]; then
        run_protected cp -p "$src/config.yaml" "$AGENT_CONFIG_FILE" || return 1
        chmod 0640 "$AGENT_CONFIG_FILE"
    fi
    if [ -f "$src/particeps-agent.service" ]; then
        run_protected cp -p "$src/particeps-agent.service" "$AGENT_UNIT_FILE" || return 1
    fi
    if [ -f "$src/state.db" ]; then
        run_protected cp -p "$src/state.db" "$(state_db_path)" || return 1
    fi
    if [ -f "$src/metrics.db" ]; then
        run_protected cp -p "$src/metrics.db" "$(metrics_db_path)" || return 1
    fi
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload 2>/dev/null || true
    fi
    return 0
}

restore_service_state() {
    command -v systemctl >/dev/null 2>&1 || return 0
    if [ "$AGENT_WAS_ACTIVE" = 1 ]; then
        run_protected systemctl restart "$AGENT_SERVICE" || return 1
        sleep 1
        systemctl is-active --quiet "$AGENT_SERVICE" || return 1
    else
        run_protected systemctl stop "$AGENT_SERVICE" 2>/dev/null || true
    fi
    return 0
}

persist_materials() {
    local dest root
    root="$BACKUP_ROOT"
    mkdir -p "$root" 2>/dev/null || root="$LOG_DIR"
    mkdir -p "$root"
    dest=$(mktemp -d "$root/interrupt-XXXXXX")
    if [ -n "$PRE_OP_DIR" ] && [ -d "$PRE_OP_DIR" ]; then
        cp -a "$PRE_OP_DIR/." "$dest/" 2>/dev/null || true
    fi
    TASK_MATERIALS="$dest"
}

rollback_fresh() {
    local failed=0 leftovers=""
    printf '正在撤销本次安装…\n'
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl stop "$AGENT_SERVICE" 2>/dev/null || true
        run_protected systemctl disable "$AGENT_SERVICE" 2>/dev/null || true
    fi
    if [ "$EXISTED_BINARY" != 1 ]; then
        run_protected rm -f "$AGENT_BINARY" || failed=1
    fi
    if [ "$EXISTED_UNIT" != 1 ]; then
        run_protected rm -f "$AGENT_UNIT_FILE" || failed=1
    fi
    if [ "$CREATED_CONFIG" = 1 ]; then
        run_protected rm -f "$AGENT_CONFIG_FILE" || failed=1
    fi
    if [ "$CREATED_DATA" = 1 ]; then
        run_protected rm -rf "$AGENT_DATA_DIR" || failed=1
    elif [ "$WROTE_ADMIN" = 1 ] && [ -f "$(state_db_path)" ]; then
        (
            trap '' INT TERM
            python3 - "$(state_db_path)" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
con.execute("DELETE FROM admin")
con.commit()
PY
        ) || failed=1
    fi
    if [ "$CREATED_PROJECT" = 1 ]; then
        run_protected incus project delete "$INCUS_PROJECT" >/dev/null 2>&1 || failed=1
    fi
    if [ "$CREATED_NETWORK" = 1 ]; then
        run_protected incus network delete "$INCUS_NETWORK" >/dev/null 2>&1 || failed=1
    elif [ "$NETWORK_V6_CHANGED" = 1 ]; then
        if [ -n "$NETWORK_V6_PREV" ]; then
            run_protected incus network set "$INCUS_NETWORK" ipv6.address="$NETWORK_V6_PREV" >/dev/null 2>&1 || failed=1
        else
            run_protected incus network set "$INCUS_NETWORK" ipv6.address=none ipv6.nat=false >/dev/null 2>&1 || failed=1
        fi
    fi
    if [ "$CREATED_POOL" = 1 ]; then
        run_protected incus storage delete "$INCUS_POOL" >/dev/null 2>&1 || failed=1
    fi
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload 2>/dev/null || true
    fi
    printf '正在移除本次新增的软件包…\n'
    if ! remove_new_packages; then
        failed=1
        leftovers="${leftovers}软件包 "
    fi
    printf '正在检查清理结果…\n'
    if [ "$EXISTED_BINARY" != 1 ] && [ -f "$AGENT_BINARY" ]; then
        failed=1
        leftovers="${leftovers}Agent 程序 "
    fi
    if [ "$CREATED_CONFIG" = 1 ] && [ -f "$AGENT_CONFIG_FILE" ]; then
        failed=1
        leftovers="${leftovers}配置 "
    fi
    if [ "$failed" = 1 ]; then
        TASK_ERROR="移除本次新增的软件包失败，或文件未能完整清理。"
        TASK_UNCONFIRMED="${leftovers% }"
        persist_materials
        printf '\n安装已停止，但回退未完成。\n\n未恢复的项目：\n  %s\n' "${TASK_UNCONFIRMED:-未知}"
        [ -z "$TASK_MATERIALS" ] || printf '恢复材料：%s\n' "$TASK_MATERIALS"
        [ -z "$TASK_LOG" ] || printf '日志：%s\n' "$TASK_LOG"
        return 1
    fi
    printf '安装已取消，本次新增的文件、资源和软件包已清理。\n'
    return 0
}

rollback_update() {
    if [ "$AGENT_REPLACED" != 1 ]; then
        printf '更新已取消。\n'
        return 0
    fi
    printf '正在恢复原版本…\n'
    printf '正在检查恢复结果…\n'
    if [ -z "$UPGRADE_BACKUP" ] || ! restore_files_from "$UPGRADE_BACKUP"; then
        TASK_ERROR="恢复备份失败。"
        persist_materials
        printf '更新已停止，但恢复原版本失败。\n'
        return 1
    fi
    if ! restore_service_state; then
        TASK_ERROR="Agent 状态未确认：未能恢复到更新前的运行状态。"
        persist_materials
        printf '更新已停止，但恢复原版本失败。\n'
        return 1
    fi
    printf '更新已取消，已恢复原版本。\nAgent：%s\n' "$(agent_state_label)"
    return 0
}

rollback_restore_op() {
    printf '正在撤销本次恢复…\n'
    printf '正在检查恢复结果…\n'
    if [ -z "$PRE_OP_DIR" ] || ! restore_files_from "$PRE_OP_DIR"; then
        TASK_ERROR="未能恢复操作前的状态。"
        persist_materials
        printf '恢复操作已停止，但未能恢复操作前的状态。\n'
        return 1
    fi
    if ! restore_service_state; then
        TASK_ERROR="Agent 状态未确认：未能恢复操作前的服务状态。"
        persist_materials
        printf '恢复操作已停止，但未能恢复操作前的状态。\n'
        return 1
    fi
    printf '已取消恢复备份，操作前的状态已恢复。\n'
    return 0
}

rollback_keep_agent() {
    printf '正在恢复 Agent…\n'
    printf '正在检查恢复结果…\n'
    if [ -z "$PRE_OP_DIR" ] || ! restore_files_from "$PRE_OP_DIR"; then
        TASK_ERROR="Agent 未能完整恢复。"
        persist_materials
        printf '卸载已停止，但 Agent 未能完整恢复。\n'
        return 1
    fi
    if ! restore_service_state; then
        TASK_ERROR="Agent 状态未确认：未能恢复操作前的服务状态。"
        persist_materials
        printf '卸载已停止，但 Agent 未能完整恢复。\n'
        return 1
    fi
    printf '卸载已取消，Agent 已恢复。\nAgent：%s\n' "$(agent_state_label)"
    return 0
}

perform_rollback_exit() {
    local failed=0
    ROLLBACK_IN_PROGRESS=1
    set +e
    trap - ERR
    case "$ACTION:$INSTALL_KIND" in
        install:update) rollback_update || failed=1 ;;
        install:*) rollback_fresh || failed=1 ;;
        rollback:*) rollback_restore_op || failed=1 ;;
        uninstall:*) rollback_keep_agent || failed=1 ;;
        *)
            RESULT_KIND="cancelled"
            exit 130
            ;;
    esac
    TASK_RETRY=0
    if [ "$failed" = 1 ]; then
        RESULT_KIND="rollback_failed"
        exit 131
    fi
    RESULT_KIND="cancelled"
    exit 130
}

apply_backup_dir() {
    local target="$1"
    mkdir -p "$AGENT_BIN_DIR" "$AGENT_CONFIG_DIR" "$(dirname "$AGENT_UNIT_FILE")" "$AGENT_DATA_DIR"
    run_protected cp -p "$target/particeps-agent" "$AGENT_BINARY" || die "恢复备份失败：无法写入程序。"
    chmod 0755 "$AGENT_BINARY"
    run_protected cp -p "$target/config.yaml" "$AGENT_CONFIG_FILE" || die "恢复备份失败：无法写入配置。"
    chmod 0640 "$AGENT_CONFIG_FILE"
    run_protected cp -p "$target/particeps-agent.service" "$AGENT_UNIT_FILE" || die "恢复备份失败：无法写入服务文件。"
    run_protected cp -p "$target/state.db" "$(state_db_path)" || die "恢复备份失败：无法写入管理数据。"
    if [ -f "$target/metrics.db" ]; then
        run_protected cp -p "$target/metrics.db" "$(metrics_db_path)" || die "恢复备份失败：无法写入监控数据。"
    fi
}

do_rollback() {
    progress "检查备份…"
    acquire_lock
    local target
    target="${1:-}"
    [ -n "$target" ] || target=$(cat "$BACKUP_ROOT/latest" 2>/dev/null || true)
    [ -n "$target" ] && [ -d "$target" ] || die "未找到可用的升级备份。"
    [ -f "$target/particeps-agent" ] || die "备份不完整，缺少：程序。"
    [ -f "$target/config.yaml" ] || die "备份不完整，缺少：配置。"
    [ -f "$target/particeps-agent.service" ] || die "备份不完整，缺少：服务文件。"
    [ -f "$target/state.db" ] || die "备份不完整，缺少：管理数据。"
    if systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
        AGENT_WAS_ACTIVE=1
    fi
    progress "保存当前状态…"
    save_agent_pre_op
    check_interrupt
    progress "恢复程序和配置…"
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl stop "$AGENT_SERVICE" 2>/dev/null || true
    fi
    apply_backup_dir "$target"
    check_interrupt
    progress "恢复管理数据…"
    check_interrupt
    progress "检查恢复结果…"
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload 2>/dev/null || true
        restore_service_state || die "Agent 状态未确认：恢复后服务状态与操作前不一致。"
    fi
    check_interrupt
    RESULT_KIND="success"
    printf '\n备份已恢复。\n\n备份：%s\nAgent：%s\n' "$target" "$(agent_state_label)"
}

confirm_purge() {
    if [ -n "$MENU_SESSION_DIR" ]; then
        return 0
    fi
    if [ "$NON_INTERACTIVE" = "1" ]; then
        [ "$CONFIRM_WORD" = "PURGE" ] || die "非交互全部卸载需要 --confirm PURGE"
        return
    fi
    local answer
    printf '\n全部卸载 particeps\n\n开始后无法用 Ctrl+C 取消。\n\n'
    menu_read '输入 PURGE 确认，其他输入返回：' answer || die "未确认卸载。"
    [ "$answer" = "PURGE" ] || die "未确认卸载。"
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
            progress "删除实例 ${name}…"
            run_protected incus --project "$INCUS_PROJECT" stop --force "$name" >/dev/null 2>&1 || true
            if ! run_protected incus --project "$INCUS_PROJECT" delete --force "$name" >/dev/null 2>&1; then
                log "强制删除失败: $name（第 ${attempt} 次）"
            fi
        done <<< "$names"
    done
    remaining=$(instance_names "$INCUS_PROJECT")
    [ -z "$remaining" ] || die "以下实例删除失败：${remaining}"
}

delete_particeps_resources() {
    command -v incus >/dev/null 2>&1 || return 0
    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        run_protected incus --project "$INCUS_PROJECT" profile device remove default eth0 >/dev/null 2>&1 || true
        run_protected incus --project "$INCUS_PROJECT" profile device remove default root >/dev/null 2>&1 || true
    fi
    if incus network show "$INCUS_NETWORK" >/dev/null 2>&1; then
        progress "删除网桥…"
        run_protected incus network delete "$INCUS_NETWORK" || die "删除网桥失败：${INCUS_NETWORK} 可能仍被其他项目使用。"
    fi
    if incus storage show "$INCUS_POOL" >/dev/null 2>&1; then
        progress "删除存储池…"
        run_protected incus storage delete "$INCUS_POOL" || die "删除存储池失败：${INCUS_POOL}。"
    fi
    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        progress "删除 particeps 项目…"
        run_protected incus project delete "$INCUS_PROJECT" || die "删除 particeps 项目失败。"
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
    PURGE_INCUS_RUNNING=1
    INTERRUPT_POLICY="ignore"
    progress "停止 Incus…"
    run_protected systemctl stop incus incus.socket incus-user incus-user.socket 2>/dev/null || true
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        progress "卸载 Incus 软件包…"
        run_protected apt-get purge -y incus incus-base incus-client incus-extra || die "卸载 Incus 软件包失败。"
        progress "清理相关依赖…"
        run_protected apt-get autoremove -y || die "清理相关依赖失败。"
        run_protected apt-get purge -y lxcfs || die "卸载 lxcfs 失败。"
    fi
    progress "删除 Incus 数据…"
    run_protected umount /var/lib/lxcfs 2>/dev/null || true
    run_protected rm -rf /var/lib/incus /var/log/incus /etc/incus /run/incus /var/cache/incus /var/lib/lxcfs
    progress "检查清理结果…"
    RESULT_KIND="success"
    printf '\nIncus 已卸载，数据已清理。\n'
}

prompt_remove_incus() {
    local answer
    if [ "$NON_INTERACTIVE" = "1" ]; then
        printf '\n已保留 Incus。\n'
        return
    fi
    if ! command -v incus >/dev/null 2>&1; then
        printf '\nparticeps 已卸载，未检测到 Incus。\n'
        return
    fi
    printf '\nparticeps 已卸载，Incus 仍保留。\n\n'
    print_foreign_instances || true
    printf '\n开始后无法用 Ctrl+C 取消。\n\n'
    INTERRUPT_POLICY="static"
    trap 'exit_script' INT
    while true; do
        menu_read '卸载 Incus 并清除全部数据？[y/N]：' answer || { printf '\n已保留 Incus。\n'; return; }
        case "$answer" in
            Y|y)
                INTERRUPT_POLICY="ignore"
                trap 'on_interrupt' INT
                purge_incus
                return
                ;;
            N|n|"")
                printf '\n已保留 Incus。\n'
                return
                ;;
            *) printf '请输入 y 或 n。\n' ;;
        esac
    done
}

remove_agent_files() {
    progress "停止 Agent…"
    if command -v systemctl >/dev/null 2>&1; then
        if [ -f "$AGENT_UNIT_FILE" ] || systemctl is-active --quiet "$AGENT_SERVICE"; then
            run_protected systemctl stop "$AGENT_SERVICE" || die "无法停止 Agent：服务未能停止。"
            if systemctl is-active --quiet "$AGENT_SERVICE"; then
                die "无法停止 Agent：服务仍在运行。"
            fi
        fi
        progress "移除 Agent 服务…"
        run_protected systemctl disable "$AGENT_SERVICE" 2>/dev/null || true
    fi
    check_interrupt
    progress "移除 Agent 程序…"
    run_protected rm -f "$AGENT_UNIT_FILE"
    run_protected rm -rf "$AGENT_BIN_DIR"
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload
    fi
    check_interrupt
}

do_uninstall() {
    acquire_lock
    if [ "$KEEP_INSTANCES" = "1" ]; then
        if systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
            AGENT_WAS_ACTIVE=1
        fi
        save_agent_pre_op
        remove_agent_files
        progress "检查卸载结果…"
        check_interrupt
        RESULT_KIND="success"
        printf '\nAgent 已卸载。\n\n实例、网络和存储池已保留。\n配置：%s\n管理数据：%s\n' "$AGENT_CONFIG_DIR" "$AGENT_DATA_DIR"
        return
    fi
    confirm_purge
    INTERRUPT_POLICY="ignore"
    trap 'on_interrupt' INT
    progress "停止 Agent…"
    if command -v systemctl >/dev/null 2>&1; then
        if [ -f "$AGENT_UNIT_FILE" ] || systemctl is-active --quiet "$AGENT_SERVICE"; then
            run_protected systemctl stop "$AGENT_SERVICE" || die "无法停止 Agent：服务未能停止。"
        fi
        run_protected systemctl disable "$AGENT_SERVICE" 2>/dev/null || true
    fi
    delete_particeps_instances
    delete_particeps_resources
    if incus project show "$INCUS_PROJECT" >/dev/null 2>&1; then
        progress "删除 particeps 项目…"
        run_protected incus project delete "$INCUS_PROJECT" || die "删除 particeps 项目失败。"
    fi
    progress "移除程序和服务…"
    run_protected rm -f "$AGENT_UNIT_FILE"
    run_protected rm -rf "$AGENT_BIN_DIR"
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload
    fi
    progress "删除配置和管理数据…"
    run_protected rm -rf "$AGENT_CONFIG_DIR" "$AGENT_DATA_DIR"
    progress "检查卸载结果…"
    RESULT_KIND="success"
    printf '\nparticeps 已卸载。\n'
    if [ -n "$MENU_SESSION_DIR" ]; then
        printf '1\n' > "$TASK_RESULT_DIR/incus_prompt"
        return
    fi
    prompt_remove_incus
}

do_update() {
    INSTALL_KIND="update"
    progress "检查更新条件…"
    preflight_existing
    acquire_lock
    local staged="$TASK_WORK_DIR/agent"
    if systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
        AGENT_WAS_ACTIVE=1
    fi
    check_interrupt
    download_agent "$staged"
    progress "备份当前安装…"
    task_state '已创建升级备份' 1
    backup_existing
    check_interrupt
    progress "替换 Agent…"
    task_state "已开始替换 Agent" 0
    run_protected mv "$staged" "$AGENT_BINARY"
    chmod 0755 "$AGENT_BINARY"
    AGENT_REPLACED=1
    check_interrupt
    progress "检查服务状态…"
    if [ "$AGENT_WAS_ACTIVE" = "1" ]; then
        progress "启动 Agent…"
        run_protected systemctl restart "$AGENT_SERVICE" || die "Agent 启动失败：服务未能重启。"
        verify_active_service
        check_interrupt
        printf '\n更新完成。\n\nAgent：运行中\n更新前备份：%s\n' "$UPGRADE_BACKUP"
    else
        check_interrupt
        printf '\n更新完成。\n\nAgent：已停止\n更新前备份：%s\n' "$UPGRADE_BACKUP"
    fi
    RESULT_KIND="success"
}

do_fresh() {
    INSTALL_KIND="fresh"
    local missing
    progress "检查安装环境…"
    missing=$(missing_install_parts)
    if [ -n "$missing" ]; then
        die "检测到不完整的安装，缺少：${missing}。"
    fi
    [ "$UPDATE_ONLY" = "0" ] || die "未检测到完整安装，无法执行更新。"
    acquire_lock
    if [ "$SKIP_HOST" != "1" ]; then
        host_preflight
        if [ -z "$POOL_SIZE" ] && { ! command -v incus >/dev/null 2>&1 || ! incus storage show "$INCUS_POOL" >/dev/null 2>&1; }; then
            prompt_pool_size || die "未设置存储池大小"
        fi
        install_packages
    fi
    check_interrupt
    local staged="$TASK_WORK_DIR/agent"
    download_agent "$staged"
    if [ "$SKIP_HOST" != "1" ]; then
        ensure_incus_resources
        setup_cgroup
    fi
    check_interrupt
    progress "写入程序和配置…"
    if [ -f "$AGENT_BINARY" ]; then EXISTED_BINARY=1; fi
    if [ -f "$AGENT_UNIT_FILE" ]; then EXISTED_UNIT=1; fi
    if [ ! -d "$AGENT_DATA_DIR" ]; then CREATED_DATA=1; fi
    mkdir -p "$AGENT_BIN_DIR" "$AGENT_DATA_DIR/backups"
    chmod 0755 "$AGENT_BIN_DIR"
    chmod 0700 "$AGENT_DATA_DIR" "$AGENT_DATA_DIR/backups"
    run_protected mv "$staged" "$AGENT_BINARY"
    chmod 0755 "$AGENT_BINARY"
    if [ ! -f "$AGENT_CONFIG_FILE" ]; then
        write_default_config
        CREATED_CONFIG=1
    else
        printf '使用现有配置。\n'
    fi
    write_unit
    check_interrupt
    progress "设置管理员密码…"
    init_admin
    check_interrupt
    if command -v systemctl >/dev/null 2>&1; then
        run_protected systemctl daemon-reload
        progress "启动 Agent…"
        run_protected systemctl enable --now "$AGENT_SERVICE"
        progress "检查服务状态…"
        verify_active_service
        check_interrupt
    fi
    check_interrupt
    RESULT_KIND="success"
    print_finish "$INSTALL_ADMIN_PASSWORD" "$INSTALL_ADMIN_EXISTING"
}

verify_active_service() {
    sleep 1
    if ! systemctl is-active --quiet "$AGENT_SERVICE"; then
        die "Agent 启动后已停止。"
    fi
}

execute_action() {
    TASK_STEP="准备操作"
    TASK_STATE=""
    TASK_ERROR=""
    TASK_RETRY=1
    TASK_WORK_DIR=""
    TASK_LOG=""
    TASK_LOG_PID=""
    UPGRADE_BACKUP=""
    SOFT_INTERRUPT=0
    INTERRUPT_NOTICE=0
    ROLLBACK_IN_PROGRESS=0
    INSTALL_KIND=""
    RESULT_KIND=""
    AGENT_WAS_ACTIVE=0
    AGENT_REPLACED=0
    CREATED_POOL=0
    CREATED_NETWORK=0
    CREATED_PROJECT=0
    CREATED_CONFIG=0
    CREATED_DATA=0
    WROTE_ADMIN=0
    NETWORK_V6_CHANGED=0
    PRE_OP_DIR=""
    TASK_DONE=""
    TASK_UNCONFIRMED=""
    TASK_MATERIALS=""
    PURGE_INCUS_RUNNING=0
    case "$ACTION" in
        uninstall)
            if [ "$KEEP_INSTANCES" = "1" ]; then
                INTERRUPT_POLICY="soft"
            else
                INTERRUPT_POLICY="ignore"
            fi
            ;;
        purge-incus) INTERRUPT_POLICY="ignore"; PURGE_INCUS_RUNNING=1 ;;
        status) INTERRUPT_POLICY="query" ;;
        start-incus|stop-incus|start-agent|stop-agent) INTERRUPT_POLICY="service" ;;
        *) INTERRUPT_POLICY="soft" ;;
    esac
    trap 'finish_task "$?"' EXIT
    trap 'unexpected_error "$?" "$LINENO"' ERR
    trap 'on_interrupt' INT
    trap 'on_interrupt' TERM
    require_root
    mkdir -p "$LOG_DIR" "$(dirname "$LOCK_FILE")"
    chmod 0700 "$LOG_DIR"
    TASK_LOG=$(mktemp "$LOG_DIR/$(date +%Y%m%d-%H%M%S)-${ACTION}-XXXXXX")
    chmod 0600 "$TASK_LOG"
    # Diagnostics go to the log file only. stdout is the user-facing terminal.
    exec 8>&2
    exec 2>>"$TASK_LOG"
    TASK_LOG_PID=""
    TASK_WORK_DIR=$(mktemp -d)
    case "$ACTION" in
        status)
            show_status
            RESULT_KIND="success"
            ;;
        start-incus) control_service Incus incus.service start ;;
        stop-incus) control_service Incus incus.service stop ;;
        start-agent) control_service Agent "$AGENT_SERVICE" start ;;
        stop-agent) control_service Agent "$AGENT_SERVICE" stop ;;
        rollback) do_rollback ;;
        uninstall) do_uninstall ;;
        purge-incus) purge_incus ;;
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
