#!/usr/bin/env bash
# poolnext-install.sh — PoolNeXt 二进制安装/更新脚本
#
# 用法:
#   ./poolnext-install.sh install                # 首次安装
#   ./poolnext-install.sh update                 # 更新到最新版
#   ./poolnext-install.sh update v1.0.1          # 更新到指定版本
#   ./poolnext-install.sh uninstall              # 卸载（停服务、删单元、删目录）
#   ./poolnext-install.sh status                 # 查看服务状态与 WebUI 地址
#   ./poolnext-install.sh start|stop|restart     # 启动/停止/重启服务
#
# 环境变量:
#   POOLNEXT_HOME   安装目录（默认 $HOME/poolnext）
#   POOLNEXT_REPO   GitHub 仓库（默认 Qalxry/PoolNeXt）
#   POOLNEXT_PORT   监听端口（默认 17778）
#
# 行为:
#   - 自动识别 linux/amd64、linux/arm64
#   - 安装/更新后若检测到 systemd 则注册并启动服务，否则 nohup 后台运行
#   - 数据文件（数据库、日志、上传）与二进制同目录
#   - SESSION_SECRET 优先取环境变量，否则首次自动生成并持久化到 0600 文件
#   - 所有下载走环境变量代理（https_proxy 等），curl 自动跟随
#   - 首次启动的一次性 root 密码安装后会在提示中给出查看方式
#     （新版 stdout 打印；v1.0.0 及更早写 poolnext-bootstrap.txt）

set -euo pipefail

HOME_DIR="${POOLNEXT_HOME:-$HOME/poolnext}"
REPO="${POOLNEXT_REPO:-Qalxry/PoolNeXt}"
PORT="${POOLNEXT_PORT:-17778}"
BINARY_NAME="poolnext-server"
BINARY_PATH="$HOME_DIR/$BINARY_NAME"
SERVICE_NAME="poolnext"
DATA_DIR="$HOME_DIR/data"
SECRET_FILE="$HOME_DIR/poolnext-secret"
DOWNLOAD_URL=""
GITHUB_API="https://api.github.com"

log()  { printf '\033[32m[poolnext]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[poolnext]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[poolnext]\033[0m %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "需要 curl 才能下载"
command -v jq  >/dev/null 2>&1 || die "需要 jq 才能解析版本信息"

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) die "不支持的架构: $(uname -m)，仅支持 amd64/arm64" ;;
    esac
}

# 解析 GitHub Release 下载 URL（不使用 gh 命令，纯 API + curl）
resolve_download_url() {
    local version="$1" asset json message
    asset="poolnext-server-linux-$(detect_arch)"
    if [[ "$version" == "latest" ]]; then
        json="$(curl -fsSL "$GITHUB_API/repos/$REPO/releases/latest" 2>/dev/null || true)"
    else
        json="$(curl -fsSL "$GITHUB_API/repos/$REPO/releases/tags/$version" 2>/dev/null || true)"
    fi
    [[ -n "$json" ]] || die "无法从 GitHub 获取 $REPO 的版本信息（网络或 API 问题），请检查网络与代理"
    message="$(printf '%s' "$json" | jq -r '.message // empty')"
    [[ -z "$message" ]] || die "获取版本失败: $message"
    DOWNLOAD_URL="$(printf '%s' "$json" | jq -r --arg a "$asset" '.assets[] | select(.name == $a) | .browser_download_url' 2>/dev/null || true)"
    [[ -n "$DOWNLOAD_URL" ]] || die "版本 $version 没有找到 $asset 资产"
    log "下载地址: $DOWNLOAD_URL"
}

# 下载并安装二进制，下载到临时文件后校验 --version 再原子替换
install_binary() {
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' RETURN
    log "下载中..."
    curl -fSL --retry 3 --retry-delay 2 -o "$tmp" "$DOWNLOAD_URL"
    chmod +x "$tmp"
    local installed_version
    if ! installed_version="$("$tmp" --version 2>/dev/null)"; then
        die "下载的二进制无法执行（架构不匹配或文件损坏）"
    fi
    log "已下载版本: $installed_version"
    mkdir -p "$HOME_DIR"
    mv "$tmp" "$BINARY_PATH"
}

# 确保 SESSION_SECRET 已配置：优先用环境变量，其次复用密钥文件，否则首次生成 32 字节随机密钥持久化到 0600 文件
ensure_secret() {
    local secret
    if [[ -n "${SESSION_SECRET:-}" ]]; then
        [[ ${#SESSION_SECRET} -ge 32 ]] || die "环境变量 SESSION_SECRET 过短（至少 32 字节）"
        log "使用环境变量 SESSION_SECRET"
        return
    fi
    if [[ -f "$SECRET_FILE" ]]; then
        secret="$(cat "$SECRET_FILE")"
        if [[ ${#secret} -lt 32 ]]; then
            die "SESSION_SECRET 文件过短，请检查 $SECRET_FILE"
        fi
    else
        secret="$(openssl rand -hex 32)"
        umask 077
        printf '%s\n' "$secret" > "$SECRET_FILE"
        umask 022
        log "已生成 SESSION_SECRET 并保存到 $SECRET_FILE（0600）"
    fi
    SESSION_SECRET="$secret"
}

# 端口占用检测：ss 或 /proc/net/tcp 都可用
port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -tln | awk '{print $4}' | grep -q ":${port}$"
    else
        grep -q "0A" /proc/net/tcp 2>/dev/null && awk '{print $2}' /proc/net/tcp 2>/dev/null | grep -q "$(printf '%04X' "$port")"
    fi
}

# 端口冲突递增探测并固定：若默认端口被占用，逐个递增找空闲端口
# （systemd 场景：端口固定，首次确定后不再变化）
resolve_port() {
    local candidate="$PORT" max=20
    if port_in_use "$candidate"; then
        local n
        for ((n = 0; n < max; n++)); do
            candidate=$((PORT + n + 1))
            if ! port_in_use "$candidate"; then
                warn "端口 $PORT 被占用，改用 $candidate"
                PORT="$candidate"
                return 0
            fi
        done
        die "端口 $PORT 至 $((PORT + max)) 均被占用，请设置 POOLNEXT_PORT"
    fi
}

# 判断 systemd 是否可用：degraded（有失败单元）时也能正常管理服务
systemd_available() {
    command -v systemctl >/dev/null 2>&1 || return 1
    local state
    state="$(systemctl is-system-running 2>/dev/null || true)"
    [[ "$state" == "running" || "$state" == "degraded" ]]
}

# systemd 可用则注册服务（以当前用户运行 + 加固），否则 nohup 后台运行
ensure_service() {
    if systemd_available; then
        mkdir -p "$HOME_DIR" "$DATA_DIR"
        cat > "/tmp/$SERVICE_NAME.service" <<EOF
[Unit]
Description=PoolNeXt Proxy Pool Control Plane
After=network.target

[Service]
Type=simple
User=$USER
ExecStart=$BINARY_PATH --port $PORT --log-dir $DATA_DIR
Environment=SESSION_SECRET=$SESSION_SECRET
WorkingDirectory=$HOME_DIR
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
        sudo install -m 0644 "/tmp/$SERVICE_NAME.service" "/etc/systemd/system/$SERVICE_NAME.service"
        sudo systemctl daemon-reload
        sudo systemctl enable "$SERVICE_NAME" --now
        log "已通过 systemd 启动服务（$SERVICE_NAME，用户 $USER）"
    else
        log "未检测到 systemd，使用 nohup 后台运行"
        cd "$HOME_DIR"
        SESSION_SECRET="$SESSION_SECRET" nohup "$BINARY_PATH" --port "$PORT" --log-dir "$DATA_DIR" \
            >> "$HOME_DIR/poolnext.log" 2>&1 &
        log "已启动，PID: $!，日志: $HOME_DIR/poolnext.log"
    fi
}

# 首次安装：创建目录、下载、启动
install() {
    if [[ -f "$BINARY_PATH" ]]; then
        warn "已存在 $BINARY_PATH，将覆盖为最新版"
    fi
    mkdir -p "$HOME_DIR" "$DATA_DIR"
    # systemd 场景端口固定，先探测冲突避免绑定失败
    systemd_available && resolve_port
    resolve_download_url "latest"
    install_binary
    ensure_secret
    log "已安装到: $BINARY_PATH"
    ensure_service
    # 兼容两种版本行为：新版二进制 stdout 打印；旧版（v1.0.0）写 bootstrap 文件
    if [[ -f "$HOME_DIR/poolnext-bootstrap.txt" ]]; then
        log "首次登录一次性密码: cat $HOME_DIR/poolnext-bootstrap.txt"
    else
        local password=""
        local attempt
        for attempt in {1..30}; do
            if systemd_available; then
                password="$(journalctl -u "$SERVICE_NAME" --since "1 min ago" --no-pager 2>/dev/null | sed -n 's/.*一次性密码: \([^ ]*\).*/\1/p' | tail -n1)"
            else
                password="$(sed -n 's/.*一次性密码: \([^ ]*\).*/\1/p' "$HOME_DIR/poolnext.log" 2>/dev/null | tail -n1)"
            fi
            [[ -n "$password" ]] && break
            sleep 1
        done
        if [[ -n "$password" ]]; then
            log "用户名: root"
            log "首次登录一次性密码: $password"
        else
            if systemd_available; then
                log "首次登录一次性密码: journalctl -u $SERVICE_NAME -n 60 | grep '一次性密码'"
            else
                log "首次登录一次性密码: grep '一次性密码' $HOME_DIR/poolnext.log"
            fi
        fi
    fi
    log "WebUI 访问地址: http://<本机IP>:${PORT}/ （用户名 root，密码见上方）"
    log "本机直接访问: http://127.0.0.1:${PORT}/"
}

# 状态：输出服务状态与 WebUI 访问地址
status() {
    local state="未知" pid="" port="$PORT" url=""
    if systemd_available && systemctl list-unit-files "$SERVICE_NAME.service" >/dev/null 2>&1; then
        state="$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || echo inactive)"
        pid="$(systemctl show -p MainPID --value "$SERVICE_NAME" 2>/dev/null)"
        # 从 systemd 单元解析实际监听端口（可能已被递增探测固定）
        local exec_start
        exec_start="$(systemctl show -p ExecStart --value "$SERVICE_NAME" 2>/dev/null)"
        if [[ "$exec_start" =~ --port[[:space:]]+([0-9]+) ]]; then
            port="${BASH_REMATCH[1]}"
        fi
    elif [[ -f "$BINARY_PATH" ]]; then
        if pid="$(pgrep -f "$BINARY_PATH" 2>/dev/null | head -n1)"; then
            state="running"
        else
            state="stopped"
        fi
    fi
    url="http://127.0.0.1:${port}/"
    log "服务: $SERVICE_NAME"
    log "状态: $state"
    [[ -n "$pid" && "$pid" != "0" ]] && log "PID: $pid"
    log "二进制: $BINARY_PATH"
    if [[ -x "$BINARY_PATH" ]]; then
        log "版本: $("$BINARY_PATH" --version 2>/dev/null || echo unknown)"
    fi
    log "本机访问: $url"
    log "WebUI 访问: http://<本机IP>:${port}/ （用户名 root）"
}

# 启动服务：systemd 优先，否则 nohup 后台
start() {
    if systemd_available && systemctl list-unit-files "$SERVICE_NAME.service" >/dev/null 2>&1; then
        sudo systemctl start "$SERVICE_NAME"
        log "已启动 systemd 服务（$SERVICE_NAME）"
    else
        if [[ ! -x "$BINARY_PATH" ]]; then
            die "未安装二进制，请先运行 install"
        fi
        if pgrep -f "$BINARY_PATH" >/dev/null 2>&1; then
            log "服务已在运行"
            return 0
        fi
        ensure_secret
        cd "$HOME_DIR"
        SESSION_SECRET="$SESSION_SECRET" nohup "$BINARY_PATH" --port "$PORT" --log-dir "$DATA_DIR" \
            >> "$HOME_DIR/poolnext.log" 2>&1 &
        log "已启动，PID: $!，日志: $HOME_DIR/poolnext.log"
    fi
    status
}

# 停止服务：systemd 优先，否则按 PID 终止
stop() {
    if systemd_available && systemctl list-unit-files "$SERVICE_NAME.service" >/dev/null 2>&1; then
        sudo systemctl stop "$SERVICE_NAME"
        log "已停止 systemd 服务（$SERVICE_NAME）"
    else
        local pid
        if pid="$(pgrep -f "$BINARY_PATH" 2>/dev/null | head -n1)"; then
            kill "$pid"
            log "已停止进程（PID: $pid）"
        else
            log "服务未在运行"
        fi
    fi
}

# 重启服务（start 内部会打印 status）
restart() {
    stop
    sleep 1
    start
}

# 卸载：停服务、移除 systemd 单元、删除部署目录
uninstall() {
    if systemd_available && systemctl list-unit-files "$SERVICE_NAME.service" >/dev/null 2>&1; then
        log "正在停止并移除 systemd 服务（$SERVICE_NAME）..."
        sudo systemctl disable --now "$SERVICE_NAME" || warn "停用服务失败，请检查权限"
        sudo rm -f "/etc/systemd/system/$SERVICE_NAME.service"
        sudo systemctl daemon-reload
    fi
    if [[ -d "$HOME_DIR" ]]; then
        log "正在删除部署目录: $HOME_DIR"
        rm -rf "$HOME_DIR"
    else
        warn "未找到部署目录: $HOME_DIR（可能已删除）"
    fi
    log "卸载完成。数据（数据库、日志、上传）已随目录一并删除。"
}

# 更新：比较当前版本与最新版本，需要更新才执行
update() {
    local target="${1:-latest}" current latest_version
    if [[ -f "$BINARY_PATH" ]]; then
        current="$("$BINARY_PATH" --version 2>/dev/null || echo none)"
    else
        current="none"
    fi
    if [[ "$target" == "latest" ]]; then
        latest_version=$(curl -fsSL "$GITHUB_API/repos/$REPO/releases/latest" | jq -r '.tag_name')
        if [[ "$current" == "$latest_version" ]]; then
            log "已是最新版 ($current)，无需更新"
            # 若此前以 nohup 安装、现在系统启用了 systemd，则补注册服务
            if systemd_available && ! systemctl is-enabled "$SERVICE_NAME" >/dev/null 2>&1; then
                log "检测到 systemd 且服务未注册，正在注册并接管"
                ensure_secret
                ensure_service
            fi
            return 0
        fi
        log "发现新版本: $current -> $latest_version"
    else
        latest_version="$target"
        if [[ "$current" == "$target" ]]; then
            log "已是指定版本 ($current)"
            return 0
        fi
        log "更新到指定版本: $current -> $target"
    fi
    # 检测到新版本，提示确认
    log "是否继续更新? [y/N]"
    read -r answer
    case "$answer" in
        y|Y|yes|YES) : ;;
        *) log "已取消更新"; return 0 ;;
    esac
    resolve_download_url "$latest_version"
    # 先停服务，避免替换二进制时 nohup 产生双进程
    stop
    install_binary
    ensure_secret
    start
    log "更新完成: $latest_version"
}

main() {
    case "${1:-}" in
        install)   install ;;
        update)    update "${2:-latest}" ;;
        uninstall) uninstall ;;
        status)    status ;;
        start)     start ;;
        stop)      stop ;;
        restart)   restart ;;
        *) die "用法: $0 {install|update [版本]|uninstall|status|start|stop|restart}" ;;
    esac
}

main "$@"
