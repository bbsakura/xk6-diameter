# xk6-diameter
Client extension for interacting with a use Diameter proto of your k6 test.

🚧 This project WIP... 🚧

## Prepair
require asdf installed.
[how to asdf install](https://asdf-vm.com/guide/getting-started.html#_2-download-asdf)

Install tools required for development.
```shell=
make install-dev-pkg
```

## Build
```shell=
make install-go-tools
make build
```

## Test Running
```shell
./out/bin/xk6-diameter run example/sample-stress.js

./out/bin/hss
```

## Support scenario

## Developers Settings

```shell
# fmt, lint, commitmessage validate...etc checker
pre-commit install

# mob programing
co-author hook > .git/hooks/prepare-commit-msg
chmod +x .git/hooks/prepare-commit-msg
```