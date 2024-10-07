# Changelog

Going forward from v0.6.0 (the first new version after the move to https://github.com/Backblaze/blazer), all notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- Nothing at present

## [0.7.1] - 2024-10-07

### Fixed

- The `cleanup` utility now deletes the `replication-target` test bucket

### Changed

- Bumped dependencies in response to dependabot alerts

## [0.7.0] - 2024-10-04

### Added

- Can now specify bucket type when listing buckets
- Can now get and set default encryption configuration, object lock, CORS rules, etc on bucket ([djenriquez](https://github.com/djenriquez))
- Can now get the S3 API URL from a bucket ([celskeggs](https://github.com/celskeggs))

### Fixed

- The `cleanup` utility now successfully deletes test files and buckets after an interrupted test run

### Changed

- Migrated to Backblaze B2 Native API v3 

## [0.6.1] - 2023-10-16

### Added

- `go.mod` file, license report ([tzeejay](https://github.com/tzeejay))

### Fixed

- Resolve import errors ([tzeejay](https://github.com/tzeejay))

### Changed

- Reference license report from README ([tzeejay](https://github.com/tzeejay))

## [0.6.0] - 2023-09-26

Tagged initial version at https://github.com/Backblaze/blazer

[unreleased]: https://github.com/Backblaze/blazer/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/Backblaze/blazer/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/Backblaze/blazer/compare/v0.5.3...v0.6.0
