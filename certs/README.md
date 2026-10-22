# certs/

`extra-ca.crt` 仅用于**本地构建/运行**时信任公司代理（如 Zscaler）的 CA。

- 仓库中提交的是空文件；本地把 PEM 内容粘贴进去即可。
- 构建时仅追加到 tools/test 阶段的 CA bundle；正式产品（runtime 镜像）不包含该证书。
- 本地运行时 `compose.local.yaml` 将本目录挂载进容器并通过 `SSL_CERT_DIR` 让 Go 信任它（`make up` 在文件非空时自动启用）。
- 本地修改不要提交，执行一次：

```sh
git update-index --skip-worktree certs/extra-ca.crt
```
