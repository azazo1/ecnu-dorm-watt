package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

func (a *App) newServerTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "管理服务端 token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var createName, createScope string
	create := &cobra.Command{
		Use:   "create",
		Short: "创建 token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTokenCreate(createName, createScope)
		},
	}
	create.Flags().StringVar(&createName, "name", "", "token 名称")
	create.Flags().StringVar(&createScope, "scope", "read,write", "权限范围, 逗号分隔")

	list := &cobra.Command{
		Use:   "list",
		Short: "列出 token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTokenList()
		},
	}

	var revokeName string
	revoke := &cobra.Command{
		Use:   "revoke",
		Short: "撤销 token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTokenRevoke(revokeName)
		},
	}
	revoke.Flags().StringVar(&revokeName, "name", "", "token 名称")

	var rotateName, rotateScope string
	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "轮换 token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTokenRotate(rotateName, rotateScope)
		},
	}
	rotate.Flags().StringVar(&rotateName, "name", "", "token 名称")
	rotate.Flags().StringVar(&rotateScope, "scope", "read,write", "新 token 的权限范围, 逗号分隔")

	cmd.AddCommand(create, list, revoke, rotate)
	return cmd
}

func (a *App) runTokenCreate(name, rawScopes string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return usageErrorf("必须提供 --name")
	}
	scopes, err := parseTokenScopes(rawScopes)
	if err != nil {
		return usageErrorf("%v", err)
	}
	token, hash, prefix, err := generateToken()
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	cfg, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	created, err := db.CreateToken(context.Background(), name, prefix, hash, scopes, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return exitErr(ExitGeneral, fmt.Errorf("token 名称已存在: %s", name))
		}
		return exitErr(ExitGeneral, fmt.Errorf("创建 token: %w", err))
	}
	if a.opts.json {
		return a.printJSON(map[string]any{
			"name":   created.Name,
			"token":  token,
			"scopes": created.Scopes,
		})
	}
	fmt.Fprintln(a.stdout, "token 已创建, 请立即保存, 之后不会再次显示:")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, token)
	fmt.Fprintln(a.stdout)
	fmt.Fprintf(a.stdout, "name:   %s\n", created.Name)
	fmt.Fprintf(a.stdout, "scopes: %s\n", strings.Join(created.Scopes, ","))
	fmt.Fprintf(a.stdout, "database: %s\n", cfg.Storage.Path)
	return nil
}

func (a *App) runTokenList() error {
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	tokens, err := db.ListTokens(context.Background())
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	if a.opts.json {
		return a.printJSON(map[string]any{"tokens": tokens})
	}
	rows := make([][]string, 0, len(tokens))
	for _, token := range tokens {
		status := "active"
		if token.RevokedAt != nil {
			status = "revoked"
		}
		rows = append(rows, []string{
			token.Name,
			token.Prefix,
			strings.Join(token.Scopes, ","),
			formatTime(token.CreatedAt, time.Local),
			formatTimePtr(token.LastUsedAt, time.Local),
			status,
		})
	}
	return a.printTable(a.stdout, []string{"NAME", "PREFIX", "SCOPES", "CREATED", "LAST USED", "STATUS"}, rows)
}

func (a *App) runTokenRevoke(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return usageErrorf("必须提供 --name")
	}
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RevokeToken(context.Background(), name, time.Now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return exitErr(ExitGeneral, fmt.Errorf("token 不存在: %s", name))
		}
		return exitErr(ExitGeneral, fmt.Errorf("撤销 token: %w", err))
	}
	if a.opts.json {
		return a.printJSON(map[string]any{"ok": true, "name": name})
	}
	fmt.Fprintf(a.stdout, "token 已撤销: %s\n", name)
	return nil
}

func (a *App) runTokenRotate(name, rawScopes string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return usageErrorf("必须提供 --name")
	}
	scopes, err := parseTokenScopes(rawScopes)
	if err != nil {
		return usageErrorf("%v", err)
	}
	token, hash, prefix, err := generateToken()
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	rotated, err := db.RotateToken(context.Background(), name, prefix, hash, scopes, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return exitErr(ExitGeneral, fmt.Errorf("token 不存在: %s", name))
		}
		return exitErr(ExitGeneral, fmt.Errorf("轮换 token: %w", err))
	}
	if a.opts.json {
		return a.printJSON(map[string]any{
			"name":   rotated.Name,
			"token":  token,
			"scopes": rotated.Scopes,
		})
	}
	fmt.Fprintln(a.stdout, "token 已轮换, 旧 token 立即失效. 新 token 只显示一次:")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, token)
	fmt.Fprintln(a.stdout)
	fmt.Fprintf(a.stdout, "name:   %s\n", rotated.Name)
	fmt.Fprintf(a.stdout, "scopes: %s\n", strings.Join(rotated.Scopes, ","))
	return nil
}

func parseTokenScopes(raw string) ([]string, error) {
	seen := map[string]bool{}
	var scopes []string
	for _, part := range strings.Split(raw, ",") {
		scope := strings.ToLower(strings.TrimSpace(part))
		if scope == "" {
			continue
		}
		if scope != "read" && scope != "write" {
			return nil, fmt.Errorf("未知 scope: %s", scope)
		}
		if !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, errors.New("至少需要一个 scope")
	}
	return scopes, nil
}

func generateToken() (token, hash, prefix string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("生成随机 token 失败: %w", err)
	}
	token = "dw_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(sum[:])
	if len(token) >= 7 {
		prefix = token[:7]
	} else {
		prefix = token
	}
	return token, hash, prefix, nil
}
