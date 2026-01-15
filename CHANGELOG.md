# Changelog

# [0.10.0](https://github.com/goliatone/go-users/compare/v0.9.0...v0.10.0) - (2026-01-15)

## <!-- 13 -->📦 Bumps

- Bump version: v0.10.0 ([12eb79e](https://github.com/goliatone/go-users/commit/12eb79e57fcdc3fec4e8166fb57fabe3e705cdd5))  - (goliatone)

## <!-- 16 -->➕ Add

- New filter fields ([fa8331a](https://github.com/goliatone/go-users/commit/fa8331a684045769de5b7cca9ab3f289e68fcb1c))  - (goliatone)
- Crud accepts channels and channel deny list ([64cc9a9](https://github.com/goliatone/go-users/commit/64cc9a98bf09e4a4bb0ad7f5340fc305a940e229))  - (goliatone)
- New masked words ([50ca4ad](https://github.com/goliatone/go-users/commit/50ca4ad2b4fce65b5294e6e0a94ac03538224256))  - (goliatone)
- Role aware scope intersects with actor scope ([13312ef](https://github.com/goliatone/go-users/commit/13312ef30de1dd7400c0b981770226a3f862c7ec))  - (goliatone)
- Machine activity exclusion run in sql ([fae4de8](https://github.com/goliatone/go-users/commit/fae4de8e804a627b0dff54d9b1431886f6eaa128))  - (goliatone)
- New activity access policy ([abf0eaa](https://github.com/goliatone/go-users/commit/abf0eaaf88623be88978ed7633f7dd0a1928a7dd))  - (goliatone)
- Activity access policy to queries ([2a03182](https://github.com/goliatone/go-users/commit/2a03182a292ede50507af085c4455f84450f0c16))  - (goliatone)
- Channel deny list and channels allow ([4a97463](https://github.com/goliatone/go-users/commit/4a974630fe860635206a0e02bfbc0fb854ea68cb))  - (goliatone)
- Policy to activity service ([bd4175e](https://github.com/goliatone/go-users/commit/bd4175e4101701cfc25e01ebaf0df6086eb9512b))  - (goliatone)
- Activity filtering with deny list ([828e62f](https://github.com/goliatone/go-users/commit/828e62f72e5b29f9a5dbbe166719100886bc6f4b))  - (goliatone)
- Activity filter helpers ([255d611](https://github.com/goliatone/go-users/commit/255d611acdb63889953cc18c728d5a0f8f1771fe))  - (goliatone)
- Activity sanitizer ([ffc5a8f](https://github.com/goliatone/go-users/commit/ffc5a8fd48ac5f8f863a9c3975b852f67fe81763))  - (goliatone)
- Activity cursor pagination ([a2a626c](https://github.com/goliatone/go-users/commit/a2a626c73ee361702a051fdfa8bdf0b311816740))  - (goliatone)
- Activity access policy ([967a2c7](https://github.com/goliatone/go-users/commit/967a2c7d2672e6d4f03aed4b2e43badbc00e32a7))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.9.0 ([e5d6d9c](https://github.com/goliatone/go-users/commit/e5d6d9c8fed59d044b5c694f04327ab6101c825a))  - (goliatone)

## <!-- 30 -->📝 Other

- PR [#1](https://github.com/goliatone/go-users/pull/1): enforce role aware scope and machine filtering in feeds and stats ([ccd4268](https://github.com/goliatone/go-users/commit/ccd42680f1edb2b53e4e9c272f799ebb7cb22a9e))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update tests ([6bf40c4](https://github.com/goliatone/go-users/commit/6bf40c4021aab97b710d4e3ff457e36c27324df9))  - (goliatone)
- Update deps ([d99f6c4](https://github.com/goliatone/go-users/commit/d99f6c4d69cb9a99a74c68655572d70d20f1bf36))  - (goliatone)
- Update docs ([dfb5705](https://github.com/goliatone/go-users/commit/dfb57053a4cfb6a72040c9dc19e5d1c679a932f8))  - (goliatone)

# [0.9.0](https://github.com/goliatone/go-users/compare/v0.8.0...v0.9.0) - (2026-01-13)

## <!-- 13 -->📦 Bumps

- Bump version: v0.9.0 ([5adde72](https://github.com/goliatone/go-users/commit/5adde720af822a3a44650ac005015aaab683c07a))  - (goliatone)

## <!-- 16 -->➕ Add

- Cache service option ([f7485d9](https://github.com/goliatone/go-users/commit/f7485d947f31b5460129d88e1a2f7e63c5a01a45))  - (goliatone)
- Use cache invalidation using tags ([767d28f](https://github.com/goliatone/go-users/commit/767d28f0440cd3aa67138782b793bf9d219d44ff))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.8.0 ([2969be0](https://github.com/goliatone/go-users/commit/2969be021fb4299e2d6587a10e61a402fadbfe49))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update deps ([a0b579c](https://github.com/goliatone/go-users/commit/a0b579c0eec69e9cf746f6249bb6ce4df5edf657))  - (goliatone)
- Update docs ([ce178b0](https://github.com/goliatone/go-users/commit/ce178b0e761160c1ef6ab7209ae3ade315b7be6f))  - (goliatone)

# [0.8.0](https://github.com/goliatone/go-users/compare/v0.7.0...v0.8.0) - (2026-01-13)

## <!-- 1 -->🐛 Bug Fixes

- Return page total for redorcs ([96ffd03](https://github.com/goliatone/go-users/commit/96ffd0337567bd357bf17a1c5e1bd8df29a8146b))  - (goliatone)
- Enforce lifecycle transitions during updates ([e168711](https://github.com/goliatone/go-users/commit/e168711faea2ab71493005cd21fdafd3361b60c7))  - (goliatone)
- Guard nil dependency ([27a3ea1](https://github.com/goliatone/go-users/commit/27a3ea10fa8856095fd71921b9b016ac889fe095))  - (goliatone)
- Make activity filters trea user/actor as OR ([faae728](https://github.com/goliatone/go-users/commit/faae728efc17052118684cf21a3f1180daee285e))  - (goliatone)

## <!-- 13 -->📦 Bumps

- Bump version: v0.8.0 ([c2a200b](https://github.com/goliatone/go-users/commit/c2a200b78ddc356d7a557d003b41bb2af36b0ff0))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.7.0 ([8da3657](https://github.com/goliatone/go-users/commit/8da3657d4379053d6cde246780b3afce7853f4e4))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update examples ([f91c9ae](https://github.com/goliatone/go-users/commit/f91c9ae0e9c1327cb4c6923411b211f5cf294690))  - (goliatone)
- Update tests ([85ebda4](https://github.com/goliatone/go-users/commit/85ebda4db1455551b5d206756bc6888171589005))  - (goliatone)

# [0.7.0](https://github.com/goliatone/go-users/compare/v0.6.0...v0.7.0) - (2026-01-13)

## <!-- 13 -->📦 Bumps

- Bump version: v0.7.0 ([85103ed](https://github.com/goliatone/go-users/commit/85103ed862c97e4fbb0a506f83e38dc2c6aeb0ea))  - (goliatone)

## <!-- 16 -->➕ Add

- New service command ([f03da03](https://github.com/goliatone/go-users/commit/f03da034d6618ae1f5d1ecb6b2f4c83a36ad64f8))  - (goliatone)
- Resolve auth use status ([3231c77](https://github.com/goliatone/go-users/commit/3231c77fd73c47e0d0f8a1cc4e38bc6c9ff99812))  - (goliatone)
- Bulk import command ([c31536a](https://github.com/goliatone/go-users/commit/c31536a95d62f72a3e69b7b56714a167af55e719))  - (goliatone)
- Activity track for create ([6cedc0c](https://github.com/goliatone/go-users/commit/6cedc0c6990d3dcfdc20d539b2dce5890a0c120b))  - (goliatone)
- Go bulk error ([c66d7a3](https://github.com/goliatone/go-users/commit/c66d7a305373888c0ca630b76fc89b64ad8f8c9f))  - (goliatone)
- Repository cache support to prefs ([95059fa](https://github.com/goliatone/go-users/commit/95059fa05c911cc9a63b2f41aa2b41a474a37d2f))  - (goliatone)
- Preferences options repository ([29705a9](https://github.com/goliatone/go-users/commit/29705a9f6771ee3d3c543dffc6f3f7f441082845))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.6.0 ([2b4d973](https://github.com/goliatone/go-users/commit/2b4d973f7a1428c1771cec7fc5010e8bdf17bed5))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update deps ([37cdca2](https://github.com/goliatone/go-users/commit/37cdca25dd8a2c0b4c18576430dd3a43baab3d40))  - (goliatone)
- Update docs ([4ca853b](https://github.com/goliatone/go-users/commit/4ca853b8d30a34e2758b10e581838f62e70623e6))  - (goliatone)
- Update guides ([a6e4612](https://github.com/goliatone/go-users/commit/a6e461240a91a925d966101f2d14df16baf3511c))  - (goliatone)
- Update tests ([775d070](https://github.com/goliatone/go-users/commit/775d070da86e99b51263fec6eaf719f0af77dd4b))  - (goliatone)

# [0.6.0](https://github.com/goliatone/go-users/compare/v0.5.0...v0.6.0) - (2026-01-12)

## <!-- 1 -->🐛 Bug Fixes

- Clone auth user ([0e69e05](https://github.com/goliatone/go-users/commit/0e69e05895b450f1253b29232c8c9f30327b2638))  - (goliatone)

## <!-- 13 -->📦 Bumps

- Bump version: v0.6.0 ([ca3e5d2](https://github.com/goliatone/go-users/commit/ca3e5d296ec5a54ec8652acde3c6120fa7a394a9))  - (goliatone)

## <!-- 16 -->➕ Add

- User actions ([1736fe2](https://github.com/goliatone/go-users/commit/1736fe2795d666fe716ce1eedcb79a7ee4c3306a))  - (goliatone)
- User create and update commands ([550fbbe](https://github.com/goliatone/go-users/commit/550fbbe36402d83b506e935d3554e412ec0650ec))  - (goliatone)
- User service implement crud ([8943e91](https://github.com/goliatone/go-users/commit/8943e9178f26f3042344b9f5b8391ddbb6b55d05))  - (goliatone)
- Include username to auth user ([07aa323](https://github.com/goliatone/go-users/commit/07aa32360f2ae356cd396729d9cc1281a83f2d09))  - (goliatone)
- Error for user and email req ([e523336](https://github.com/goliatone/go-users/commit/e52333650e12f1cfed1d39aea8c5b61c73253700))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.5.0 ([c5e886b](https://github.com/goliatone/go-users/commit/c5e886bc6ecce46c91acf3968e42be3b52328fbc))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update examples ([a35f941](https://github.com/goliatone/go-users/commit/a35f941a8aa2a7ddd8f5d47d887dfe3f0329680a))  - (goliatone)
- Update tests ([7070cdc](https://github.com/goliatone/go-users/commit/7070cdc729fd41b4da128e658b2d62f7e266ef9b))  - (goliatone)
- Update deps ([5044e0f](https://github.com/goliatone/go-users/commit/5044e0f0c0e3e5c2220a7bb7020e3f222574afcc))  - (goliatone)
- Update guides ([4e7a182](https://github.com/goliatone/go-users/commit/4e7a182f79bb022f1ec98c56432ec611bc8d5aa2))  - (goliatone)

# [0.5.0](https://github.com/goliatone/go-users/compare/v0.4.0...v0.5.0) - (2026-01-09)

## <!-- 13 -->📦 Bumps

- Bump version: v0.5.0 ([99a0cf4](https://github.com/goliatone/go-users/commit/99a0cf4c7a2261fdf92af5fe4167d0eed297497e))  - (goliatone)

## <!-- 16 -->➕ Add

- Order field to custom roles ([f312711](https://github.com/goliatone/go-users/commit/f312711c16276b68988e418d53a1421e6b92293a))  - (goliatone)
- Curd service ([4e7b201](https://github.com/goliatone/go-users/commit/4e7b20176fd707c8e2c521290034cf4d953c3ec4))  - (goliatone)
- Migration for order field ([eedd795](https://github.com/goliatone/go-users/commit/eedd795488ab2f0611d20a3019460432f1a0b08f))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.4.0 ([19b4dda](https://github.com/goliatone/go-users/commit/19b4dda6b86b9c839004e475d8d7acdcf2b434ff))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update docs ([b487b27](https://github.com/goliatone/go-users/commit/b487b27eceb6029f4fa91236deb6c24698cdaef3))  - (goliatone)
- Update tests ([946243a](https://github.com/goliatone/go-users/commit/946243a3cb9d73403b6a584d0106e1d0ebc60168))  - (goliatone)
- Update deps ([04a0cbe](https://github.com/goliatone/go-users/commit/04a0cbe5663ca7d569fdc49d296fb3a884fdca4b))  - (goliatone)

# [0.4.0](https://github.com/goliatone/go-users/compare/v0.3.0...v0.4.0) - (2026-01-08)

## <!-- 13 -->📦 Bumps

- Bump version: v0.4.0 ([e61479a](https://github.com/goliatone/go-users/commit/e61479a2102099708d68ced6d7b247eebecbba53))  - (goliatone)

## <!-- 16 -->➕ Add

- Crud service include new fields ([dceedda](https://github.com/goliatone/go-users/commit/dceedda3bf052d84c7f94318b325ca4b8e6560d3))  - (goliatone)
- Role create command ([e7f3754](https://github.com/goliatone/go-users/commit/e7f3754636a7f4849cc6cd5e147966a1fb0b332d))  - (goliatone)
- Update custom role model ([bc8a509](https://github.com/goliatone/go-users/commit/bc8a5096a465eade4edb526e98def0a5f9d588b1))  - (goliatone)
- Migrations to register custom roles meta and role_key ([264f5ae](https://github.com/goliatone/go-users/commit/264f5aeee6ca0ddc8357397033178539b37fb7ab))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.3.0 ([41c53fe](https://github.com/goliatone/go-users/commit/41c53fee4b75f448d98c20f036ec7509fe780fd2))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update readme ([96a278f](https://github.com/goliatone/go-users/commit/96a278f8df60b49034cd381c06941dbbc6b2c7b1))  - (goliatone)
- Update examples ([683316f](https://github.com/goliatone/go-users/commit/683316f8b47b8b525de400da3bc4a3878f9a4d08))  - (goliatone)
- Update tests ([8710f60](https://github.com/goliatone/go-users/commit/8710f60bc0c9ec02c0d6206adaac6f95451d6f61))  - (goliatone)

# [0.3.0](https://github.com/goliatone/go-users/compare/v0.2.0...v0.3.0) - (2025-12-02)

## <!-- 13 -->📦 Bumps

- Bump version: v0.3.0 ([8536432](https://github.com/goliatone/go-users/commit/85364320e40f1749d05ea6df1b4ee386cf42a60b))  - (goliatone)

## <!-- 16 -->➕ Add

- Command adapter ([59cee85](https://github.com/goliatone/go-users/commit/59cee85f0efb8bec97b11f6de538ea901565bbf5))  - (goliatone)
- Activity helper ([03f5e94](https://github.com/goliatone/go-users/commit/03f5e94b217f7b0284e12300e415b8852d107345))  - (goliatone)

## <!-- 3 -->📚 Documentation

- Update changelog for v0.2.0 ([09f89cd](https://github.com/goliatone/go-users/commit/09f89cd78abda425159361dbb6aecca398d2910c))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update docs ([3a54aed](https://github.com/goliatone/go-users/commit/3a54aed8e27d34d126ba43ca4cec9779ad2ee584))  - (goliatone)
- Update gitignore ([59e50cd](https://github.com/goliatone/go-users/commit/59e50cda10e7ff480102820c514d1b20afa21347))  - (goliatone)

# [0.2.0](https://github.com/goliatone/go-users/tree/v0.2.0) - (2025-11-25)

## <!-- 1 -->🐛 Bug Fixes

- Example show data ([ad43ddf](https://github.com/goliatone/go-users/commit/ad43ddfe1d15a71e156316b376e6d6ee65ba030e))  - (goliatone)
- Example updated to reflect deps new api ([83dc704](https://github.com/goliatone/go-users/commit/83dc70429d584d2ecfdb8c3ad297ef786567ee58))  - (goliatone)
- Use file for db ([d660312](https://github.com/goliatone/go-users/commit/d6603126a700cf94d3d9d607620c70e236dd19ad))  - (goliatone)
- Implement repository interface ([1f4df86](https://github.com/goliatone/go-users/commit/1f4df86a7e26b8e2a3c0632c13fd382114eac8e9))  - (goliatone)
- Clone request ([a82c515](https://github.com/goliatone/go-users/commit/a82c515b590616945c80567112e38933bd57425d))  - (goliatone)
- Go options correct path ([9f49746](https://github.com/goliatone/go-users/commit/9f497463e466b78ac099fafd2b63864605c68556))  - (goliatone)

## <!-- 13 -->📦 Bumps

- Bump version: v0.2.0 ([4c0c71f](https://github.com/goliatone/go-users/commit/4c0c71f610de78e45f72cdc8ff29a66edf65a632))  - (goliatone)

## <!-- 16 -->➕ Add

- Activity helpers ([c141d08](https://github.com/goliatone/go-users/commit/c141d08c119b19edf3f79d9f0d3f1f88a2341810))  - (goliatone)
- Custom roles ([c2c5ecb](https://github.com/goliatone/go-users/commit/c2c5ecb09c60e7cc99651ed80b3872e67158c60a))  - (goliatone)
- Preference records ([33a4941](https://github.com/goliatone/go-users/commit/33a4941a0b92dcff964be63111d3a8e3fb5e336c))  - (goliatone)
- Scope filters ([ffe6dbf](https://github.com/goliatone/go-users/commit/ffe6dbf1ec094cce9776623b46ca59f570fae769))  - (goliatone)
- Activity record ([23de234](https://github.com/goliatone/go-users/commit/23de234c779fcc66a1b871bf5e53d21fefac088a))  - (goliatone)
- Validation for query ([58f3ba2](https://github.com/goliatone/go-users/commit/58f3ba2e2cf33b770946b6a59365a416ec843b16))  - (goliatone)
- Validation for commands ([804741b](https://github.com/goliatone/go-users/commit/804741bf2513bd0699d705bc385f8db78d2fd252))  - (goliatone)
- Crud guard ([b1fea39](https://github.com/goliatone/go-users/commit/b1fea39627dade0bfdb6b1d317d30ce8d48bf6eb))  - (goliatone)
- Crud service ([330c9b3](https://github.com/goliatone/go-users/commit/330c9b3148e8bd3368a4aca56fa314f22f868eb1))  - (goliatone)
- Go-auth converter ([c1b1cd1](https://github.com/goliatone/go-users/commit/c1b1cd118c3c083438ef86ba3cc5af753fcba32e))  - (goliatone)
- Schema helpers ([d96585c](https://github.com/goliatone/go-users/commit/d96585cfbe1ab254eb442e14ec702c62071ed564))  - (goliatone)
- Authentication helper ([7c662f2](https://github.com/goliatone/go-users/commit/7c662f24fcfb0f83b78e7167922f9e15db6afb75))  - (goliatone)
- Telemetry pkg ([3117762](https://github.com/goliatone/go-users/commit/31177629a7d7894644a034274d478a3bc10b212f))  - (goliatone)
- Actor roles ([cc2d136](https://github.com/goliatone/go-users/commit/cc2d136822b507303678ba408bca95c7ab57360e))  - (goliatone)
- Migrations ([8249f6a](https://github.com/goliatone/go-users/commit/8249f6a65513c7cd01ed26df94f1c150eecf64ea))  - (goliatone)
- Migrations for new stage ([4537b81](https://github.com/goliatone/go-users/commit/4537b81df183ddc96edb7a760664dec2afd4c2df))  - (goliatone)
- Migraiton files ([7c1139e](https://github.com/goliatone/go-users/commit/7c1139ebcac98b52d1659f802862cae2d2f1f904))  - (goliatone)
- Aut adapter ([28cb4d0](https://github.com/goliatone/go-users/commit/28cb4d070d36a6f3f03e3341c1c638c5c59459b6))  - (goliatone)
- Policy and auth integration ([6150ebc](https://github.com/goliatone/go-users/commit/6150ebce6cc341defb3a27ac0ec51c674b2c5eaa))  - (goliatone)
- User preferences ([912c293](https://github.com/goliatone/go-users/commit/912c293bd9ea2c41ecc42b2d8f13ce3aa0c3cad8))  - (goliatone)
- User profile ([c25ae75](https://github.com/goliatone/go-users/commit/c25ae7581583f698eea86caa5a0d2a0e08fe2654))  - (goliatone)
- Query functions ([be37be3](https://github.com/goliatone/go-users/commit/be37be3bca7ccd496c77da504dbb6b06c5222e97))  - (goliatone)
- Registry implementaiton ([ebd7e30](https://github.com/goliatone/go-users/commit/ebd7e307b179aebcde1787c4f978ebde9791d0c7))  - (goliatone)
- Scope for policies ([ddb0f8e](https://github.com/goliatone/go-users/commit/ddb0f8e23c5030ed276a2736dd8b51f9829a227e))  - (goliatone)
- Service implementation ([3114738](https://github.com/goliatone/go-users/commit/3114738bf8aca4378f704861f307eacd90b95e21))  - (goliatone)
- Service entrypoint ([9bb8a09](https://github.com/goliatone/go-users/commit/9bb8a09b650642c166271aa1d113d03926571dd4))  - (goliatone)
- Commands ([90e6af3](https://github.com/goliatone/go-users/commit/90e6af3743274c624752c4968a0399b83b990e2f))  - (goliatone)
- Activity tracker ([5b525a7](https://github.com/goliatone/go-users/commit/5b525a7c733915b6c0e18d4d7805ba4f26871878))  - (goliatone)

## <!-- 2 -->🚜 Refactor

- Rename bun registry ([bc51284](https://github.com/goliatone/go-users/commit/bc51284c555a7754b2ded1e101872c971c700696))  - (goliatone)

## <!-- 7 -->⚙️ Miscellaneous Tasks

- Update docs ([a120ec8](https://github.com/goliatone/go-users/commit/a120ec8445b801ad3fc697d21da241fa75570b2f))  - (goliatone)
- Update examples ([6d442b8](https://github.com/goliatone/go-users/commit/6d442b8e89203adb9607bdcd382076305c1855bf))  - (goliatone)
- Update deps ([fbe3d96](https://github.com/goliatone/go-users/commit/fbe3d969fcbaa14821a2f5086a3664972aa5291a))  - (goliatone)
- Add examples ([1f44052](https://github.com/goliatone/go-users/commit/1f440522d5a7168080ef4709fc4cd3c3ebea5d1e))  - (goliatone)
- Update readme ([d7f0408](https://github.com/goliatone/go-users/commit/d7f040842ca7fd0c13719246a9e94cc7038b2ffa))  - (goliatone)
- Update tests ([4009203](https://github.com/goliatone/go-users/commit/4009203a7629d3fb89dc6d13e73370bd2f01e1ca))  - (goliatone)
- Update migrations ([6273dec](https://github.com/goliatone/go-users/commit/6273dec0eab9694f41b2fee8556ae7f0f0cd2739))  - (goliatone)
- Add base tasks ([e0df1a4](https://github.com/goliatone/go-users/commit/e0df1a4300d4514583c208add5f5c4db0a3a8312))  - (goliatone)
- Initial commit ([3fe70f1](https://github.com/goliatone/go-users/commit/3fe70f190ecbef534a75fb9135dada86ba5d473b))  - (goliatone)


