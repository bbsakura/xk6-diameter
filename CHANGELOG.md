# Changelog

## [0.1.0](https://github.com/bbsakura/xk6-diameter/compare/v0.0.1...v0.1.0) (2025-02-28)


### Features

* add custom manager for Go tools installation script in renovate.json ([e1525ed](https://github.com/bbsakura/xk6-diameter/commit/e1525ed152e9490a8fc50edd4aaa22d1504d8e81))
* add GitHub Actions workflows for Docker image creation and release ([a8ff5f5](https://github.com/bbsakura/xk6-diameter/commit/a8ff5f53e77759c8cb878f420373ab1baed775a0))
* add go-dep-sync target to Makefile for dependency synchronization ([e95cb85](https://github.com/bbsakura/xk6-diameter/commit/e95cb8538829277a405d17f9bdc28e631e6f4d38))
* rename output binary name ([2ebeb11](https://github.com/bbsakura/xk6-diameter/commit/2ebeb115b5aa9d027c2efa7def5acf1a97f93e28))


### Bug Fixes

* add sleep before starting hss-server to ensure proper initialization ([9c4cf10](https://github.com/bbsakura/xk6-diameter/commit/9c4cf109a35d4acb258f8de0344d30fa3fb5ed78))
* **ci:** fix golangci settings ([01b6f78](https://github.com/bbsakura/xk6-diameter/commit/01b6f78de21d6d33b83364840151423ea06f218b))
* **deps:** update github.com/dop251/goja digest to 3491d4a ([e0884f1](https://github.com/bbsakura/xk6-diameter/commit/e0884f1b14848cc243e3aa82585f307c7df90594))
* **deps:** update github.com/dop251/goja digest to 5f46f27 ([91465dc](https://github.com/bbsakura/xk6-diameter/commit/91465dcbc93711894bfb31bc6ed8d233d65cfe1c))
* **deps:** update module go.k6.io/k6 to v0.57.0 ([#30](https://github.com/bbsakura/xk6-diameter/issues/30)) ([93f9b20](https://github.com/bbsakura/xk6-diameter/commit/93f9b200e508166cc6658e0abc9e83ce84d52108))
* **deps:** update module go.k6.io/xk6 to v0.13.0 ([830e144](https://github.com/bbsakura/xk6-diameter/commit/830e1443e33ad4f362f84f4dd4f7290be5ebf978))
* **deps:** update module golang.org/x/tools to v0.26.0 ([a980a21](https://github.com/bbsakura/xk6-diameter/commit/a980a21b77b19991e168cf553213878ac58de3fa))
* disable non-k6 dependency updates ([84a93ff](https://github.com/bbsakura/xk6-diameter/commit/84a93ff43dfdcbe7d629735fea071e94128e214b))
* fix pre-commit config ([2f726cd](https://github.com/bbsakura/xk6-diameter/commit/2f726cd8fa108152cdca950b2b0f25d3a12450aa))
* fix scenario ([270d7a2](https://github.com/bbsakura/xk6-diameter/commit/270d7a24ad070c5fb1fb559fb6d151ffb43bcff7))
* fix workflow ([dcbc04c](https://github.com/bbsakura/xk6-diameter/commit/dcbc04cca30f7afa2b89564b91980b2265119828))
* github workflows golangci ([#26](https://github.com/bbsakura/xk6-diameter/issues/26)) ([a9dc0c5](https://github.com/bbsakura/xk6-diameter/commit/a9dc0c50c6c9ae381703bf4ec1e995bad8d6579a))
* lint errors. ([92ce8fe](https://github.com/bbsakura/xk6-diameter/commit/92ce8fe074dab8f54b59e4c9beb6f0239fbce1ec))
* Remove deprecated golangci-lint configuration ([4563f47](https://github.com/bbsakura/xk6-diameter/commit/4563f47a015d1258945d6800638c2fd3925f0d26))
* remove golangci-lint import from tools.go ([1f44467](https://github.com/bbsakura/xk6-diameter/commit/1f44467d605e18fe7a3d0a80ee8a1be10ea79c7a))
* rename command in script ([d5b4097](https://github.com/bbsakura/xk6-diameter/commit/d5b40972a9d691f58bf50fb0aa9f222d6d00cc09))
* update go.k6.io/k6 to v0.56.0 and replace goja with grafana/sobek ([f036b14](https://github.com/bbsakura/xk6-diameter/commit/f036b145aea3c898d5628da5656d5c47aebb5785))
* update K6 and xk6 versions in Dockerfile to v0.56.0 and v0.13.4 ([a7aa4d6](https://github.com/bbsakura/xk6-diameter/commit/a7aa4d60d8ea936bb0d01df39dce8060145881ed))
