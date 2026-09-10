package azure

import "testing"

func TestContainerURL(t *testing.T) {
	tests := []struct {
		name           string
		account        string
		container      string
		endpointSuffix string
		want           string
		wantErr        bool
	}{
		{
			name:      "sufixo padrão",
			account:   "stterraform",
			container: "time1",
			want:      "https://stterraform.blob.core.windows.net/time1",
		},
		{
			name:           "cloud soberana",
			account:        "stterraform",
			container:      "time1",
			endpointSuffix: "blob.core.usgovcloudapi.net",
			want:           "https://stterraform.blob.core.usgovcloudapi.net/time1",
		},
		{name: "sem account", container: "time1", wantErr: true},
		{name: "sem container", account: "stterraform", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ContainerURL(tt.account, tt.container, tt.endpointSuffix)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("erro = nil, quero falha (got %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			if got != tt.want {
				t.Errorf("ContainerURL = %q, quero %q", got, tt.want)
			}
		})
	}
}

// O Azure trata nome de metadata como case-insensitive e não garante a
// capitalização na resposta, então a chave do lock precisa ser normalizada.
func TestNormalizeMetadata(t *testing.T) {
	valor := "abc"
	vazio := ""

	got := normalizeMetadata(map[string]*string{
		"Terraformlockid": &valor,
		"OUTRO":           &vazio,
		"nulo":            nil,
	})

	if got["terraformlockid"] != "abc" {
		t.Errorf("chave não normalizada: %+v", got)
	}
	if _, ok := got["outro"]; !ok {
		t.Errorf("valor vazio deveria ser preservado: %+v", got)
	}
	if _, ok := got["nulo"]; ok {
		t.Errorf("valor nulo deveria ser descartado: %+v", got)
	}
}

func TestNormalizeMetadataVazia(t *testing.T) {
	if got := normalizeMetadata(nil); got != nil {
		t.Errorf("normalizeMetadata(nil) = %+v, quero nil", got)
	}

	nulo := map[string]*string{"a": nil}
	if got := normalizeMetadata(nulo); got != nil {
		t.Errorf("só valores nulos deveria virar nil, veio %+v", got)
	}
}
