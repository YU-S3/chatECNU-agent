# 快速开始指南

## 前置要求

1. **安装Go** (1.21或更高版本)
   ```bash
   # Ubuntu/Debian
   sudo apt update
   sudo apt install golang-go
   
   # 或者从官网下载: https://go.dev/dl/
   ```

2. **获取ChatECNU API令牌**
   - 访问: https://developer.ecnu.edu.cn/vitepress/llm/case/userkey.html
   - 登录并生成您的API令牌
   - 妥善保管令牌，不要泄露

## 构建步骤

### 1. 进入项目目录
```bash
cd /home/yangchengyu/my_agent_project
```

### 2. 构建可执行文件
```bash
./build.sh
```

**提示**: 构建脚本已自动配置Go代理使用国内镜像源。如果遇到网络问题，可以手动设置：
```bash
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
./build.sh
```

构建成功后，会生成 `chatecnu-agent` 可执行文件。

### 3. 配置API密钥

**方式1: 使用环境变量（推荐）**
```bash
export ECNU_API_KEY='your_api_key_here'
```

**方式2: 使用.env文件**
```bash
cp env.example .env
# 使用你喜欢的编辑器编辑 .env 文件
nano .env  # 或 vim .env
# 将 ECNU_API_KEY=your_api_key_here 中的 your_api_key_here 替换为你的实际API密钥
```

### 4. 运行Agent
```bash
./chatecnu-agent
```

## 使用示例

### 示例1: 列出当前目录
```
用户> 列出当前目录的所有文件
[工具调用] list_directory
[列出目录] /home/yangchengyu/my_agent_project
[助手] 当前目录包含以下文件：
  [文件] main.go
  [文件] go.mod
  [文件] README.md
  ...
```

### 示例2: 读取文件
```
用户> 读取README.md文件的内容
[工具调用] read_file
[读取文件] /home/yangchengyu/my_agent_project/README.md
[助手] README.md文件内容如下：
...
```

### 示例3: 执行命令
```
用户> 执行 ls -la 命令
[工具调用] execute_command
[执行命令] ls -la (超时: 30秒)
[助手] 命令执行成功，输出如下：
total 48
drwxr-xr-x 3 user user 4096 ...
...
```

### 示例4: 创建文件
```
用户> 创建一个名为test.txt的文件，内容为"Hello World"
[工具调用] write_file
[写入文件] /home/yangchengyu/my_agent_project/test.txt (追加: false)
[助手] 成功创建文件test.txt
```

## 常见问题

### Q: 构建失败，提示"go: command not found"
A: 需要先安装Go。参考前置要求中的安装步骤。

### Q: API调用失败，提示"401 Unauthorized"
A: 检查API密钥是否正确设置，确保环境变量或.env文件中的密钥正确。

### Q: 命令执行失败，提示权限错误
A: 某些操作可能需要sudo权限。Agent会自动在命令前添加sudo（如果需要）。

### Q: 如何退出Agent？
A: 输入 `exit` 或 `quit` 即可退出。

## 下一步

- 查看 [README.md](README.md) 了解更多功能
- 阅读 [开发者协议](https://developer.ecnu.edu.cn/vitepress/llm/tos.html) 了解使用规范
- 探索更多工具和功能

