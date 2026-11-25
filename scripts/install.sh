#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请使用 root 权限运行安装脚本: sudo bash scripts/install.sh"
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) BIN_NAME="door-linux-amd64" ;;
  aarch64|arm64) BIN_NAME="door-linux-arm64" ;;
  *)
    echo "暂不支持的架构: $ARCH"
    exit 1
    ;;
esac

echo "[1/4] 初始化 /door 核心隔离目录..."
mkdir -p /door/server/{data,backup,java,python,logs}
mkdir -p /door/database/{mysql,postgresql,redis,mongodb}
mkdir -p /door/project
mkdir -p /door/nginx/{sbin,conf/vhost,ssl,logs}

if [ -f "./bin/${BIN_NAME}" ]; then
  echo "[2/4] 安装二进制到 /usr/local/bin/door ..."
  install -m 0755 "./bin/${BIN_NAME}" /usr/local/bin/door
else
  echo "未找到 ./bin/${BIN_NAME}，请先运行 make build-linux 构建产物"
  exit 1
fi

echo "[3/4] 写入 Systemd 服务 /etc/systemd/system/door.service ..."
cat > /etc/systemd/system/door.service <<'EOF'
[Unit]
Description=Door Lightweight Linux Ops Panel
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/door serve --addr :12345
Restart=on-failure
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

echo "[4/4] 启动并设置开机自启..."
systemctl daemon-reload
systemctl enable --now door.service

echo "=========================================="
echo "Door 运维面板安装完成！"
echo "访问地址: http://<服务器IP>:12345"
echo "默认账号: admin"
echo "默认密码: 12345678（首次登录必须修改）"
echo "=========================================="
