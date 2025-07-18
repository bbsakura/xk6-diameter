# xk6-diameter
Client extension for interacting with a Diameter protocol in your k6 test.


## Preparation

Requires mise to be installed.
[How to install mise](https://github.com/jdx/mise?tab=readme-ov-file#install-mise)

Install tools required for development.

```shell
make install-dev-pkg
```

## Build

```shell
make install-go-tools
make build
```

## Test Running
```shell
./out/bin/xk6-diameter run example/sample-stress.js

./out/bin/hss-client
./out/bin/hss-server
```

## Support scenario

## Developers Settings

```shell
# Format, lint, commit message validation, etc.
pre-commit install

# Mob programming
co-author hook > .git/hooks/prepare-commit-msg
chmod +x .git/hooks/prepare-commit-msg
```
