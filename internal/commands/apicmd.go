package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/api"
)

// newAPICmd is a raw request escape hatch, used to probe which endpoints the
// app's scopes and the user's role allow.
func newAPICmd(a *app) *cobra.Command {
	var method, data string
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Make a raw authenticated request to the 42 API",
		Example: `  ft api /v2/me
  ft api '/v2/users/swilmus/locations?filter[active]=true'
  ft api -X POST /v2/slots -d '{"slot":{"user_id":1,"begin_at":"...","end_at":"..."}}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := url.Parse(args[0])
			if err != nil {
				return err
			}
			if !strings.HasPrefix(u.Path, "/v2/") && !strings.HasPrefix(u.Path, "/oauth/token/info") {
				return errors.New("path must start with /v2/ (or be /oauth/token/info)")
			}
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			var body []byte
			if data != "" {
				if !json.Valid([]byte(data)) {
					return errors.New("--data must be valid JSON")
				}
				body = []byte(data)
			}
			resp, err := c.Do(cmd.Context(), strings.ToUpper(method), u.Path, u.Query(), body)
			if resp != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "HTTP %d", resp.StatusCode)
				if q := c.Quota(); q.Known {
					fmt.Fprintf(cmd.ErrOrStderr(), "  (quota left: %d/s, %d/h)", q.SecondlyRemaining, q.HourlyRemaining)
				}
				fmt.Fprintln(cmd.ErrOrStderr())
				printBody(a, resp)
			}
			var apiErr *api.Error
			if errors.As(err, &apiErr) {
				// Body already printed; just signal failure.
				return fmt.Errorf("HTTP %d", apiErr.StatusCode)
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", http.MethodGet, "HTTP method")
	cmd.Flags().StringVarP(&data, "data", "d", "", "JSON request body")
	return cmd
}

func printBody(a *app, resp *api.Response) {
	var buf bytes.Buffer
	if json.Indent(&buf, resp.Body, "", "  ") == nil {
		buf.WriteByte('\n')
		a.out.Write(buf.Bytes())
		return
	}
	a.out.Write(resp.Body)
}
