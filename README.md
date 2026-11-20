# http_downloader

一个用 Go 写的简易命令行下载器：支持多线程分段下载、断点续传、代理和进度条。

## 特性

- **多线程下载**：通过 HTTP Range 把文件切段并行下载
- **自动降级**：服务器不支持 Range 时，自动回退到单线程下载
- **断点续传**：分段文件保存在 `<文件名>.parts/` 目录，中断后重新运行会按已有大小续传
- **代理支持**：`http` / `https` / `socks5`，不指定时沿用系统环境变量代理
- **进度条**：实时显示下载进度和速度

## 构建

需要 Go 1.26+

```bash
go build -o downloader .
```

Windows 下：

```bash
go build -o downloader.exe .
```

## 用法

```bash
downloader -u <URL> [-o 输出路径] [-t 线程数] [-p 代理]
```

| 参数 | 简写 | 说明 | 默认值 |
| --- | --- | --- | --- |
| `--url` | `-u` | 下载链接（必填） | — |
| `--out` | `-o` | 输出文件路径 | 从 `Content-Disposition` 或 URL 推断 |
| `--thread` | `-t` | 并发线程数 | CPU 核心数 |
| `--proxy` | `-p` | 代理地址，如 `http://127.0.0.1:7890`、`socks5://127.0.0.1:1080` | 使用环境变量代理 |

## 示例

```bash
# 最简单：自动命名，线程数 = CPU 核心数
downloader -u https://example.com/file.zip

# 指定输出路径和 8 个线程
downloader -u https://example.com/file.zip -o ./downloads/file.zip -t 8

# 走 HTTP 代理
downloader -u https://example.com/file.zip -p http://127.0.0.1:7890

# 走 SOCKS5 代理，省略 http:// 也可以
downloader -u https://example.com/file.zip -p socks5://127.0.0.1:1080
```

查看帮助和版本：

```bash
downloader --help
downloader --version
```

## 工作流程

1. 先发 `HEAD` 请求获取文件大小和 `Accept-Ranges`。
2. 若服务器支持 Range，按线程数把文件切成若干段，每段一个 goroutine 并发下载。
3. 若某个分段请求拿到 `200 OK`（说明服务器忽略了 Range），取消其余请求、清理分段目录，改用单线程重新下载。
4. 全部分段完成后按顺序合并为最终文件，删除 `.parts` 目录。

## 说明

- 中断后再次运行同一命令即可续传；如果分段文件损坏或大小异常，按提示删除 `.parts` 目录后重试。
- 目前不校验文件完整性（如 MD5/Checksum），需要的话下载后可自行校验。
