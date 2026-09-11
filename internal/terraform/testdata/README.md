# Fixtures de plano

Saídas reais de `terraform show -json`, capturadas com Terraform v1.14.5. O
JSON está exatamente como o Terraform emitiu (compacto, sem reformatação) para
que os testes rodem contra o formato de verdade.

Os recursos são `terraform_data`, que é embutido no Terraform — regenerar as
fixtures não exige baixar provider nem credencial de nuvem.

| Arquivo | Conteúdo |
| --- | --- |
| `plan_mixed.json` | create, update, delete, replace e no-op no mesmo plano |
| `plan_no_changes.json` | plano sem nenhuma mudança |
| `plan_targeted_incomplete.json` | plano com `-target`, ou seja `complete: false` |
| `plan_incomplete_destructive.json` | `complete: false` **e** com um delete, para checar a precedência dos códigos de saída |

## Como regenerar

Em um diretório vazio, com este `main.tf`:

```hcl
resource "terraform_data" "keep" {
  input = "estavel"
}

resource "terraform_data" "to_update" {
  input = "antes"
}

resource "terraform_data" "to_delete" {
  input = "sera-removido"
}

resource "terraform_data" "to_replace" {
  input            = "conteudo"
  triggers_replace = "v1"
}
```

```sh
terraform init
terraform apply -auto-approve
```

Depois edite o `main.tf`: remova `to_delete`, troque o `input` de `to_update`
para `"depois"`, troque `triggers_replace` de `to_replace` para `"v2"` e
acrescente um `to_create`. Então:

```sh
terraform plan -out=tf.plan
terraform show -json tf.plan > plan_mixed.json

terraform apply -auto-approve
terraform plan -out=tf.plan
terraform show -json tf.plan > plan_no_changes.json
```

Para o plano incompleto, altere qualquer recurso e planeje com `-target`:

```sh
terraform plan -out=tf.plan -target=terraform_data.to_update
terraform show -json tf.plan > plan_targeted_incomplete.json
```

Para o plano incompleto com destruição, remova `to_create` do `main.tf` e
aponte o `-target` justamente para ele:

```sh
terraform plan -out=tf.plan -target=terraform_data.to_create
terraform show -json tf.plan > plan_incomplete_destructive.json
```

Os casos que o `terraform_data` não produz — `forget`, data sources e
combinações de ações desconhecidas — são cobertos por planos montados em
memória nos testes, não por fixtures.

# Fixtures de state

Arquivos `.tfstate` (formato v4), que é **outro formato** da saída de
`terraform show -json`: o state bruto tem `version`/`serial`/`lineage` e
`resources[].instances[]`, não `format_version`/`values`.

| Arquivo | Conteúdo |
| --- | --- |
| `state_vazio.tfstate` | state recém-criado sem recursos — **181 bytes** |
| `state_vazio_minimo.tfstate` | o mesmo, sem `check_results`, para o caso de state escrito por versão mais antiga |
| `state_com_recursos.tfstate` | módulo, `count` e data source, que são os três casos que complicam a remontagem de endereço |

Os 181 bytes do state vazio não são um número arbitrário: é o tamanho medido de
um `terraform apply` sem nenhum recurso, e é o que fundamenta o limite de
`emptyStateBytes` na detecção de state suspeito de estar vazio. No container
real inspecionado havia oito states de 180 a 183 bytes.

Para regenerar, em um diretório vazio:

```sh
echo '# sem recursos' > main.tf
terraform init && terraform apply -auto-approve   # -> state_vazio.tfstate
```

E para o que tem recursos, um `main.tf` com um `terraform_data`, um segundo com
`count = 2`, um `module` local e um `data "terraform_remote_state"`.
