# GoDynamo — Suíte de Testes Unitários (Design)

- **Data:** 2026-05-30
- **Status:** Aprovado para planejamento (aguardando revisão do spec pelo usuário)
- **Autor:** Estevao + Claude
- **Objetivo:** Construir uma rede de regressão que impeça que novas funcionalidades quebrem as existentes, elevando a cobertura de ~30% para ~65%+ com foco em lógica determinística.

---

> ## ⚠️ RESTRIÇÃO CRÍTICA DE SEGURANÇA — LEIA ANTES DE QUALQUER TESTE
>
> **Todo teste que toque na AWS DEVE usar mock/fake e NUNCA pode chamar a AWS diretamente.**
> Nenhum teste pode criar credenciais reais, abrir um cliente AWS real, nem chamar
> qualquer operação da SDK contra um endpoint da AWS (incluindo `ListTables`, `Scan`,
> `Query`, `PutItem`, `DeleteItem`, `CreateTable`, `DescribeTable`, `GetItem`,
> `DiscoverRegionsWithTables`).
>
> **Por quê:** as credenciais padrão da máquina do desenvolvedor apontam para a conta
> AWS **de produção da empresa**. Um teste que bata na AWS real pode **modificar ou
> destruir o banco de dados da empresa** — risco de demissão. Esta é a restrição de
> maior prioridade deste projeto e sobrepõe qualquer outra meta (cobertura, conveniência, etc.).
>
> **Regras concretas:**
> 1. A fronteira AWS é isolada por interface e injetada com fake (ver §4 e §7).
> 2. Nenhum teste chama `dynamo.NewClient`, `config.LoadDefaultConfig`, `dynamodb.NewFromConfig`
>    ou `DiscoverRegionsWithTables`.
> 3. Nenhum `tea.Cmd` retornado pelo `Update` que chame `m.client.*` é executado nos testes (§7).
> 4. Se uma unidade não puder ser testada sem tocar na AWS, ela fica **fora do escopo** — nunca
>    "só dessa vez". Sem exceções.
> 5. O CI roda sem credenciais AWS configuradas, como rede de segurança adicional.

---

## 1. Motivação

A cobertura atual é desigual: `query` 91.9% e `gui` 64.5% estão bem servidos, mas `models` 0%, `ui` 3.3%, `dynamo` 8.6% (apenas `profiles.go`) e `app` 0% (o `app.go` de 2233 linhas) não têm rede de proteção. Sem testes nessas áreas, qualquer refatoração ou nova feature pode quebrar silenciosamente a lógica central de conversão de dados, construção de queries e a máquina de estados da TUI.

## 2. Restrições (invioláveis)

- **Nunca** bater na AWS real nos testes — **mock obrigatório** (ver o aviso crítico no topo). As credenciais da máquina apontam para a conta de produção da empresa; um teste que chame a AWS pode destruir dados reais. O usuário roda testes ao vivo manualmente, fora desta suíte.
- Manter a convenção existente: stdlib `testing`, table-driven, **white-box** (mesmo pacote), fakes implementando interfaces. **Sem testify.**
- Não introduzir mudança de comportamento em produção, exceto:
  - o "seam" do `dynamo.Client` (§4), que é puramente aditivo;
  - as 3 correções de bug aprovadas (§6).

## 3. Estratégia e princípios

1. **Lógica > renderização.** Priorizar testes determinísticos de lógica e transição de estado. Para funções `View()` (saída com estilos lipgloss), apenas *smoke tests* (não dá panic + contém o conteúdo-chave). Asserções pixel-a-pixel são frágeis e quebram em mudanças cosméticas — o oposto de uma boa rede de regressão.
2. **Testar o que é usado.** Não perseguir cobertura em componentes mortos (ver §5, widgets `ui` não usados pelo app).
3. **Travar comportamento, depois corrigir bugs.** Onde corrigimos bugs (§6), os testes asseguram o comportamento corrigido; o resto trava o comportamento atual.
4. **Injeção de dependência via interface** para isolar a fronteira AWS, espelhando o padrão já existente em `gui.Backend`.

## 4. Refatoração de produção: "seam" no `dynamo.Client`

### Problema
`internal/dynamo/client.go:147` declara `db *dynamodb.Client` (tipo concreto). Os 9 sites de chamada (`c.db.*` em client.go:206/249/355/428/538/553/565/630/640) usam o cliente concreto, impossibilitando testes sem AWS.

### Solução
Extrair uma interface não exportada com o subconjunto exato de métodos usados (8 métodos; `Scan` é chamado 2× mas é o mesmo método):

```go
// dynamoAPI é o subconjunto de *dynamodb.Client usado por Client, extraído para
// que os testes injetem um fake sem chamar a AWS. Espelha o padrão de gui.Backend.
type dynamoAPI interface {
	ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error)
	DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	CreateTable(context.Context, *dynamodb.CreateTableInput, ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// Asserção em tempo de compilação (falha rápido se o SDK mudar assinaturas).
var _ dynamoAPI = (*dynamodb.Client)(nil)
```

- Trocar o campo `db *dynamodb.Client` → `db dynamoAPI`. `*dynamodb.Client` já satisfaz a interface, então `NewClient` **não muda** — comportamento idêntico em produção.
- Nos testes (mesmo pacote `dynamo`), construir `&Client{db: fakeAPI, region: "...", endpoint: "..."}`.
- O fake retorna structs concretos `*dynamodb.XxxOutput` (ex.: `&dynamodb.ScanOutput{Items: ...}`), o que traz o pacote SDK `types` para o arquivo de teste.

### Verificação obrigatória pós-refatoração
- `go build ./...` (o campo `db` é referenciado **somente** em client.go — confirmado via grep — então nada mais quebra).
- `go test ./...` (a suíte `gui` existente, que usa `*dynamo.Client` via `gui.Backend`, deve continuar passando — a interface `Backend` opera sobre os métodos **exportados** de `Client`, não sobre `db`).

## 5. Plano por pacote

| Pacote | Hoje | Alvo | Escopo |
|--------|------|------|--------|
| `internal/models` | 0% | ~95% | Conversões `AttributeValue↔interface{}` (todos os tipos: S/N/B/BOOL/NULL/SS/NS/BS/L/M aninhados), `JSONToItem`/`ItemToJSON` (round-trip com valores estáveis), `GetAttributeType`, `FormatValue` (incl. casos de borda corrigidos — §6.1) |
| `internal/ui` → fuzzy | 3% | ~85% | `FuzzyFind` (ordenação por score, prefixo exato, padrão vazio, sem-match), `fuzzyScore` (bônus consecutivo/separador/início), `HighlightMatches` (flush de runs match/não-match). Branch camelCase será removido (§6.3) |
| `internal/ui` → components | — | DataTable+List ~85% | **Cobrir a fundo (usados pelo app):** `DataTable` (limites do cursor Up/Down/Left/Right, `SetData`, `calculateColWidths`, `GetSelectedRow`), `List` (`MoveUp/Down`, `SetItems`, `GetSelected`). **Smoke-only/skip (não usados pelo app):** `Form`, `Tabs`, `InfoBox`, `StatusBar` |
| `internal/ui` → json_viewer | — | ~70% | Peso em `Render` (estrutura aninhada, toggle de nós) e highlight de busca; `Toggle`/`ExpandAll`/`CollapseAll`; `Format*` (leves) |
| `internal/ui` → filter | parcial | ~85% | Preencher lacunas do `filter_test.go` existente |
| `internal/dynamo` → client | 8.6% | ~70% | **Via fake (§4):** `ListTables` (paginação multi-página), `DescribeTable` (parse de chave de partição/ordenação + GSI + LSI → `TableInfo`), `ScanTable`/`QueryTable` (passagem de filtro/nomes/valores + conversão), `ScanTableContinuous` (loop até `targetCount`, cancelamento por contexto, timeout), `PutItem`/`DeleteItem`, `CreateTable` (billing PROVISIONED vs PAY_PER_REQUEST, com/sem sort key), `GetItem`, `interfaceToAttributeValue` (todos os tipos) |
| `internal/dynamo` → profiles | (ok) | manter | Já testado (`ListProfilesFromReader`, `orderProfiles`). `ListProfiles()` (wrapper de FS) fica sem teste — baixo valor sem DI |
| `internal/app` | 0% | ~45% | **Helpers puros:** `formatBytes`, `itemsToTable` (com `tableInfo` setado), `extractText`, `getSortedSelection`, `applyTableFilter`. **Handlers de mensagem:** `handleScanResult`/`handleContinuousScanResult`/`handleQueryResult` com resultados sintéticos. **Transições do `Update` dirigidas por struct de mensagem:** `WindowSizeMsg`→dimensões, `tablesLoadedMsg`→popula, `tableInfoMsg`, `scanResultMsg`, `errMsg`→erro+loading. **Smoke tests** das `viewX`. Ver §7 (regras críticas) |
| `internal/query` | 91.9% | manter (+) | Opcional: cobrir o fallback "inalcançável" e o guard `len(values)==0` em plan.go |
| `internal/gui` | 64.5% | manter | Sem novo trabalho; garantir que continua verde após o seam |
| `main` | 29.4% | ~60% | `selectMode` (casos `tui`/`gui`/default/flags pass-through). `runTUI`/`main` não são cobríveis (sobem o programa) |

**Cobertura global esperada: ~65%+** (estimativa honesta, considerando widgets `ui` não usados e os limites do `app`).

## 6. Correções de bug (aprovadas)

### 6.1 `models.FormatValue` — panic e corte UTF-8 (models.go:192)
`str[:maxLen-3]` faz **panic** quando `maxLen<3` (índice negativo) e corta no meio de runes multibyte (byte-slice). **Fix:** guarda para `maxLen` pequeno e truncamento ciente de runes (`[]rune`). Testes asseguram: maxLen 1/2/3, string multibyte (ex.: acentos/emoji) sem corromper.

### 6.2 `models.AttributeValueToInterface` — precisão de N (models.go:35)
`"5.0"`→`int64(5)` é escolha de exibição deliberada e mudá-la "rippla" para todo o UI — **não** será alterada. O risco **real** é perda de precisão de inteiros grandes (> 2^53 o `float64` corrompe). **Fix mínimo e seguro:** preservar precisão para inteiros que não cabem em float64 (parse para `int64` antes de cair para `float64`; já há um caminho int64, mas validar o limiar). Testes documentam o comportamento de N (incl. o round-trip lossy conhecido para floats integrais) e travam a não-corrupção de inteiros grandes.

### 6.3 `ui/fuzzy.go:96` — branch camelCase morto + byte-index
O texto chega sempre lowercased (fuzzy.go:31), então `unicode.IsUpper(rune(text[textIdx]))` nunca dispara, e `rune(text[...])` é byte-index incorreto. **Fix:** remover o branch morto (simplificação). Testes garantem que a remoção não altera scores observáveis.

> Os fixes são pré-requisito dos testes correspondentes: escrever o teste que expõe o bug (falha), aplicar o fix (passa) — TDD para os 3.

## 7. Regras críticas para os testes de `app` (evitar flakiness)

- **Nunca executar `tea.Cmd` que chama a AWS.** Transições que "fazem trabalho" (`scanTable`, `loadTables`, `deleteItem`, `connectToRegion`) retornam um `tea.Cmd` que chama `m.client.*`. Os testes apenas dirigem `Update` com mensagens e asseguram estado (`m.view`, `m.loading`, cmd não-nulo) — **sem invocar o Cmd retornado** (invocá-lo bateria na AWS real e travaria).
- **Não asserir sucesso de clipboard.** Os caminhos `y`/`Y` chamam `clipboard.WriteAll` (app.go:653/665/828/990/2143), que **falha em CI headless** (sem X11). Testes evitam essas teclas ou asseguram apenas estado não-relacionado a clipboard. (O yank de modo visual em :990 ignora o erro, então é seguro.)
- `m.client` continua sendo `*dynamo.Client` concreto no `app` (o seam é interno ao pacote `dynamo`); por isso a cobertura do `app` é limitada a helpers puros + handlers + transições dirigidas por mensagem.

## 8. CI + automação

### `.github/workflows/test.yml`
Dispara em `push` e `pull_request`. Matriz resolvida:
- **`ubuntu-latest`**: `go vet ./...` + `go test ./... -race -coverprofile=coverage.out` (CGO/gcc disponível por padrão → `-race` confiável e barato).
- **`windows-latest`**: `go test ./...` **sem `-race`** (pega caminhos específicos de Windows: `splash.go` usa `powershell`, separadores de path, `resource_windows.syso`). Evita a fragilidade do CGO/`-race` no runner Windows.

`actions/setup-go` com cache de módulos. Bloqueia merge se qualquer job falhar. (Sem gate rígido de % de cobertura no início — gera atrito; o número fica visível via `make cover`.)

**Salvaguarda de segurança no CI:** os jobs rodam **sem credenciais AWS** (nenhum secret AWS exposto ao workflow; `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_PROFILE` ausentes). Assim, se algum teste acidentalmente tentar tocar na AWS, ele falha por falta de credencial em vez de atingir uma conta real — uma rede de segurança a mais além do isolamento por mock.

### `Makefile`
- `make test` → `go test ./...`
- `make race` → `go test ./... -race`
- `make cover` → `go test ./... -coverprofile=coverage.out && go tool cover -html`
- `make vet` → `go vet ./...`

## 9. Fora de escopo (YAGNI)

- `internal/ui/textarea/**` (1608 linhas — componente adaptado de terceiros; baixo valor testar nós mesmos).
- `internal/ui/styles.go` (constantes de estilo lipgloss).
- Widgets `ui` não usados pelo app (`Form`, `Tabs`, `InfoBox`, `StatusBar`) — no máximo smoke.
- `DiscoverRegionsWithTables` (bate na AWS; concorrência já protegida por mutex — nada para o `-race` pegar em unit test).
- Testes de integração com Electron ou AWS real.

## 10. Riscos e mitigações

| Risco | Mitigação |
|-------|-----------|
| **🔴 CRÍTICO: teste tocar na AWS de produção e destruir dados da empresa (risco de demissão)** | Isolamento total por mock/fake (§4, §7); proibição absoluta de chamar a SDK/`NewClient`/`DiscoverRegionsWithTables` em testes; CI sem credenciais AWS; ver aviso crítico no topo |
| Refatoração do `db` quebrar algo | `db` só é usado em client.go (verificado); asserção de compilação + `go build`/`go test` completos |
| Testes de `app` baterem na AWS | Regra §7: nunca executar Cmd AWS; só dirigir mensagens |
| Flake de clipboard em CI | Regra §7: não asserir sucesso de clipboard |
| `-race` no Windows (CGO) | Windows roda sem `-race`; Ubuntu cobre o race detector |
| Round-trip N "consertado" rippling no UI | Fix #2 limitado a precisão de inteiros grandes; tipo de exibição inalterado |

## 11. Plano de verificação

1. `go build ./...` verde após o seam.
2. `go test ./...` verde (incl. suíte `gui` existente).
3. `go vet ./...` limpo.
4. `go test ./... -cover` mostra os alvos da §5 atingidos (~65%+ global).
5. Os 3 testes de bug falham **antes** do fix e passam **depois** (evidência TDD).
6. CI verde nos dois runners.

## 12. Convenções de teste (resumo para implementação)

- Arquivos `*_test.go` no **mesmo pacote** (white-box).
- Table-driven com `t.Run(name, ...)` para subtestes.
- Fakes manuais implementando interfaces (padrão `fakeBackend` em `gui/server_test.go:16`).
- Sem dependências novas no `go.mod`.
- Helpers de teste compartilhados por pacote quando reduzir duplicação (ex.: construtor de `*dynamodb.ScanOutput` sintético).
