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
