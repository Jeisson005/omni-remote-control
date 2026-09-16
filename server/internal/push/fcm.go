// Package push implementa un cliente mínimo de Firebase Cloud Messaging (FCM
// HTTP v1) usando únicamente la librería estándar de Go. Se utiliza para
// despertar a los clientes Android (Doze-friendly) cuando se requiere lectura
// de notificaciones o SMS en vivo.
//
// Configuración mediante variables de entorno:
//
//	FCM_CREDENTIALS_JSON: ruta al archivo JSON de la cuenta de servicio de
//	                      Firebase, o el propio JSON embebido como string.
//	FCM_PROJECT_ID:       (opcional) ID del proyecto. Si no se indica, se toma
//	                      del campo project_id de las credenciales.
//
// Si FCM_CREDENTIALS_JSON no está presente, todas las operaciones son no-op
// (el servidor sigue funcionando sin push).
package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type serviceAccount struct {
	ProjectID  string `json:"project_id"`
	PrivateKey string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI   string `json:"token_uri"`
}

type Client struct {
	enabled   bool
	projectID string
	email     string
	tokenURI  string
	key       *rsa.PrivateKey
	http      *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewFromEnv construye un cliente FCM a partir de las variables de entorno.
// Nunca retorna error: si la configuración falta o es inválida, devuelve un
// cliente deshabilitado que ignora los envíos.
func NewFromEnv() *Client {
	raw := strings.TrimSpace(os.Getenv("FCM_CREDENTIALS_JSON"))
	if raw == "" {
		log.Println("FCM not configured (FCM_CREDENTIALS_JSON empty). Remote wake push disabled.")
		return &Client{enabled: false, http: &http.Client{Timeout: 15 * time.Second}}
	}

	data := []byte(raw)
	if !strings.HasPrefix(raw, "{") {
		fileData, err := os.ReadFile(raw)
		if err != nil {
			log.Printf("FCM disabled: could not read credentials file %q: %v", raw, err)
			return &Client{enabled: false, http: &http.Client{Timeout: 15 * time.Second}}
		}
		data = fileData
	}

	var sa serviceAccount
	if err := json.Unmarshal(data, &sa); err != nil {
		log.Printf("FCM disabled: invalid service account JSON: %v", err)
		return &Client{enabled: false, http: &http.Client{Timeout: 15 * time.Second}}
	}

	projectID := strings.TrimSpace(os.Getenv("FCM_PROJECT_ID"))
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		log.Println("FCM disabled: incomplete credentials (project_id, client_email or private_key missing).")
		return &Client{enabled: false, http: &http.Client{Timeout: 15 * time.Second}}
	}

	key, err := parsePrivateKey(sa.PrivateKey)
	if err != nil {
		log.Printf("FCM disabled: could not parse private key: %v", err)
		return &Client{enabled: false, http: &http.Client{Timeout: 15 * time.Second}}
	}

	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	log.Printf("FCM client enabled for project %s", projectID)
	return &Client{
		enabled:   true,
		projectID: projectID,
		email:     sa.ClientEmail,
		tokenURI:  tokenURI,
		key:       key,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled
}

// SendWake envía un mensaje de datos de alta prioridad a un token FCM.
func (c *Client) SendWake(ctx context.Context, deviceToken string, data map[string]string) error {
	if !c.Enabled() || deviceToken == "" {
		return nil
	}

	accessToken, err := c.accessToken(ctx)
	if err != nil {
		return fmt.Errorf("could not obtain FCM access token: %w", err)
	}

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": deviceToken,
			"data":  data,
			"android": map[string]interface{}{
				"priority": "high",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("FCM send failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp.Add(-1*time.Minute)) {
		return c.token, nil
	}

	now := time.Now()
	header := base64URL([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]interface{}{
		"iss":   c.email,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   c.tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + base64URL(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	assertion := signingInput + "." + base64URL(signature)

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	c.token = tokenResp.AccessToken
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	c.tokenExp = now.Add(time.Duration(expiresIn) * time.Second)
	return c.token, nil
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
		return rsaKey, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func base64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
