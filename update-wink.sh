#!/bin/sh
# --- 配置变量 ---
REPO="makt28/wink"
FILE_NAME="wink-linux-amd64"
INSTALL_PATH="/usr/local/bin/wink"

echo ">>> 正在检查并下载最新版本..."

# 1. 获取最新 Release 下载地址
LATEST_URL=$(curl -s https://api.github.com/repos/$REPO/releases/latest | grep "browser_download_url" | grep "$FILE_NAME" | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo "错误：无法获取 GitHub 下载链接，请检查网络。"
    exit 1
fi

# 2. 下载到临时目录
echo "正在从 GitHub 拉取: $LATEST_URL"
curl -L "$LATEST_URL" -o "/tmp/wink_new"

if [ $? -eq 0 ]; then
    chmod +x "/tmp/wink_new"
    
    # 3. 停止服务、替换、启动
    echo "正在重启服务..."
    rc-service wink stop 2>/dev/null
    mv "/tmp/wink_new" "$INSTALL_PATH"
    rc-service wink start
    
    echo ">>> Wink 已成功更新到最新版本并启动！"
else
    echo "下载失败，放弃更新。"
    exit 1
fi
