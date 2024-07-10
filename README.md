# xk6-diameter
Client extension for interacting with a Diameter protocol in your k6 test.

🚧 This project is a work in progress... 🚧

## Preparation

Requires asdf to be installed.
[How to install asdf](https://asdf-vm.com/guide/getting-started.html#_2-download-asdf)

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
