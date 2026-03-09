#!/bin/sh
# --- 配置变量 ---
DOMAIN="demo-wink.zgentime.com"
SSL_DIR="/etc/nginx/ssl"

echo ">>> 正在执行初始化部署 (一次性任务)..."

# 1. 安装基础依赖
apk update
apk add curl nginx openssl libc6-compat

# 2. 生成自签名证书
mkdir -p $SSL_DIR
openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
  -keyout $SSL_DIR/wink.key \
  -out $SSL_DIR/wink.crt \
  -subj "/C=CN/ST=Zhejiang/L=Hangzhou/O=Zgentime/OU=Dev/CN=$DOMAIN"

# 3. 配置 Nginx (HTTPS 反向代理)
cat <<EOF > /etc/nginx/http.d/wink.conf
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl;
    server_name $DOMAIN;
    ssl_certificate $SSL_DIR/wink.crt;
    ssl_certificate_key $SSL_DIR/wink.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
rm -f /etc/nginx/http.d/default.conf

# 4. 创建 Wink 的 OpenRC 服务定义
cat <<EOF > /etc/init.d/wink
#!/sbin/openrc-run
name="wink"
description="Wink Binary Service"
command="/usr/local/bin/wink"
command_background="yes"
pidfile="/run/\${RC_SVCNAME}.pid"
depend() {
    need net
}
EOF
chmod +x /etc/init.d/wink

# 5. 设置开机自启
rc-update add nginx default
rc-update add wink default
rc-service nginx restart

echo ">>> 初始化完成！环境已就绪。"
