# План доработки devc: усиление secure-local workflow

## Context

Внешний агент-ревьюер заявил, что devc реализует ~80-85% security-модели, но указал ряд пробелов.
Три независимых Explore-агента проверили его утверждения по фактическому коду, судья свёл результаты:

- **Реальный баг:** `enforceWorkspaceSecrets` вызывается в `Up` ([manager.go:136](../internal/container/manager.go#L136)),
  но НЕ в `Exec`/`Attach` ([manager.go:397](../internal/container/manager.go#L397),
  [manager.go:413](../internal/container/manager.go#L413)). Секрет, созданный после `devc up`, доступен агенту
  через `devc exec`/`devc shell`.
- **Устаревшие комментарии-вранье:** `mode=mask is not yet implemented` в
  [manager.go:708](../internal/container/manager.go#L708) и `implemented in a later milestone` в
  [types.go:79](../pkg/types/types.go#L79) — mask уже реализован на mount-time
  ([mounts.go:92](../internal/docker/mounts.go#L92)).
- **Продуктовые решения (by design, не баги):** `secure-local-agent` использует `credentialPolicy=agentOnly`
  (Claude-креды проходят, git/cloud/ssh — нет); egress firewall opt-in через `network.enforce`.
- **Опровергнуто:** host Docker socket НЕ монтируется (единственное упоминание — комментарий-справка).

Решения пользователя: базовый `secure-local-agent` не менять; добавить отдельный strict-preset; закрыть
баг exec/attach; починить комментарии; добавить runtime-verification тесты; расширить secret-паттерны.

Цель: устранить единственный реальный security-gap, дать строгий preset для «вообще никаких host creds +
enforced egress», убрать вводящие в заблуждение комментарии и закрепить поведение тестами.

## Изменения

### 1. Закрыть gap: проверка secrets в `Exec` и `Attach` (must-fix)

В [internal/container/manager.go](../internal/container/manager.go):

- В `Exec` ([:397](../internal/container/manager.go#L397)) перед `m.Docker.ExecAs`: загрузить конфиг через
  существующий `loadMergedConfig(workspaceFolder)` ([manager.go:448](../internal/container/manager.go#L448)) и
  вызвать `enforceWorkspaceSecrets(workspaceFolder, merged)` ([manager.go:710](../internal/container/manager.go#L710)).
  Сейчас `Exec` вообще не грузит конфиг — добавить загрузку.
- В `Attach` ([:413](../internal/container/manager.go#L413)): аналогично, вызвать `enforceWorkspaceSecrets`
  перед стартом/attach. `Attach` уже частично грузит конфиг (`loadMergedConfig` в ветке stopped) — вынести
  загрузку выше, чтобы проверка работала и для running-контейнера.
- Семантика: при `mode=fail` и найденном секрете — отказ со ссылкой на `secrets.FormatFailure`, как в `Up`.
  Для `mode=off/readonly/mask` `enforceWorkspaceSecrets` уже возвращает `nil` — поведение не меняется.
- Решить деградацию конфига: если `loadMergedConfig` падает с ошибкой — в `Exec` это должно быть фатально
  (нельзя пускать без проверки), в `Attach` для уже существующего контейнера — тоже фатально для секретов
  (отличается от текущей best-effort логики запуска services, которую оставляем мягкой).

### 2. Новый preset `secure-local-strict`

В [internal/preset/preset.go](../internal/preset/preset.go):

- Добавить функцию `secureLocalStrict()` и зарегистрировать в `builders` ([:17](../internal/preset/preset.go#L17)).
- Поля: то же, что `secureLocalAgent`, но:
  - `CredentialPolicy: types.CredentialPolicyNone` — никаких host-кредов, включая Claude.
  - `Network: &types.NetworkConfig{Mode: "restricted", Enforce: true, Allowlist: [...]}` — реальный egress
    firewall. Allowlist: `api.anthropic.com`, `registry.npmjs.org`, `pypi.org`, `files.pythonhosted.org`,
    `proxy.golang.org`, `github.com` (агентские `NetworkAllow` из профилей мёржатся поверх через
    `egressDomains`, см. [manager.go:388](../internal/container/manager.go#L388)).
- Тип `NetworkConfig` — [types.go:140](../pkg/types/types.go#L140) (`Mode`, `Allowlist`, `Enforce`).
- Поскольку при `credentialPolicy=none` Claude не авторизуется с хоста — задокументировать, что в strict
  агент авторизуется внутри контейнера отдельно (container-local).

### 3. Починить устаревшие комментарии

- [manager.go:707-709](../internal/container/manager.go#L707): убрать `mode=mask is not yet implemented`;
  описать, что mask/readonly — технические контроли на mount-time (формулировка уже верна внутри функции на
  [:722-724](../internal/container/manager.go#L722)).
- [types.go:79](../pkg/types/types.go#L79): убрать `implemented in a later milestone` у `SecretsModeMask`.

### 4. Расширить default secret-паттерны

В [internal/secrets/scan.go:20](../internal/secrets/scan.go#L20) (`defaultPatterns`): добавить
`*.local.yaml`, `*.local.yml`, `*.secret.*`, `private*.json`, `*-credentials.*`, `*.pem`, `id_rsa`, `id_ed25519`.
Проверить, что они не конфликтуют с `defaultAllowPatterns` ([:34](../internal/secrets/scan.go#L34)).

### 5. Runtime-verification тесты

- **Unit (приоритет):**
  - `internal/preset/preset_test.go` — проверить поля `secure-local-strict` (credentialPolicy=none,
    Network.Enforce=true, allowlist непустой).
  - `internal/container/` — тест, что `Exec`/`Attach` отклоняют запуск при найденном секрете
    (mode=fail) — через мок Docker-клиента, по образцу существующих тестов менеджера.
  - `internal/secrets/scan_test.go` — кейсы на новые паттерны + что allowlist (`.env.example`) их не ловит.
- **Integration smoke-скрипт** (опционально, в `scripts/` или `Makefile` target, документированный, не в CI):
  `devc exec -- env | grep -E 'GH_TOKEN|GITHUB_TOKEN|GITLAB_TOKEN|SSH_AUTH_SOCK|AWS_|KUBECONFIG'` → пусто;
  `devc exec -- test ! -S /var/run/docker.sock`; `devc exec -- git push` → exit 1.

### 6. Документация

- [docs/secure-local-agent.md](secure-local-agent.md): добавить раздел про `secure-local-strict`
  (рядом с существующим «Why the secure preset uses agentOnly, not none» на :100); явно описать лимит
  `mask` («файлы, созданные после старта контейнера, не маскируются» — уже в коде на
  [mounts.go:96](../internal/docker/mounts.go#L96)); отметить, что allowlist без `enforce` — advisory.
- [README.md:158](../README.md#L158): упомянуть `--preset secure-local-strict` рядом с `secure-local-agent`.

## Чего НЕ делаем

- Не монтируем docker.sock (его и нет) — действий не нужно.
- Не меняем `secure-local-agent` (остаётся agentOnly, opt-in firewall) — по решению пользователя.
- Не усиливаем git wrapper до «абсолютной границы» (pre-push hook и т.п.) — при отсутствии git-кредов в
  strict риск низкий; вне scope.

## Verification

```bash
make test            # все unit-тесты, включая новые preset/secrets/manager
make lint            # go vet
go test ./internal/preset ./internal/secrets ./internal/container
```

После завершения реализации — запустить `/code-review` по рабочему diff и устранить найденное.

Ручная проверка strict-preset:

```bash
devc init --preset secure-local-strict --agent claude
devc up
devc exec -- env | grep -E 'GH_TOKEN|AWS_|KUBECONFIG'   # пусто
# создать .env в workspace, затем:
devc exec -- echo hi                                    # должен отказать (mode=fail)
devc exec -- git push                                   # exit 1
```
