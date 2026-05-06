package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/app"
	"github.com/rpcarvs/reasond/internal/settings"
)

type initRequest struct {
	Providers []assetbundle.Provider
	Settings  settings.Settings
}

type judgeChoice struct {
	Provider string
	Model    string
}

const providerPromptDescription = "Checkbox list: press x or space to select Codex and/or Claude Code, then press enter to confirm."

const judgePromptDescription = "Choose the default judge used by `reasond judge`. Use arrows to move and enter to confirm."

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize reasond hooks, skills, and judge defaults",
		Long: strings.TrimSpace(`
Initialize reasond in the current repository.

The command installs Codex and/or Claude Code assets, prepares .reasond state,
and saves the default judge provider/model to .reasond/settings.json.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := promptInitRequest()
			if err != nil {
				return err
			}
			return runInitRequest(cmd.OutOrStdout(), request)
		},
	}
}

func promptInitRequest() (initRequest, error) {
	providers := []assetbundle.Provider{assetbundle.ProviderCodex}
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[assetbundle.Provider]().
				Title("Install which coding-agent assets?").
				Description(providerPromptDescription).
				Options(providerOptions(providers)...).
				Filterable(false).
				Limit(2).
				Height(7).
				Validate(func(selected []assetbundle.Provider) error {
					if len(selected) == 0 {
						return fmt.Errorf("select at least one provider")
					}
					return nil
				}).
				Value(&providers),
		),
	).WithAccessible(initAccessibleMode()).WithTheme(initPromptTheme()).Run(); err != nil {
		return initRequest{}, err
	}

	choices, err := judgeChoices()
	if err != nil {
		return initRequest{}, err
	}
	selected := choices[0]
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[judgeChoice]().
				Title("Choose the default judge provider/model").
				Description(judgePromptDescription).
				Options(judgeChoiceOptions(choices)...).
				Height(10).
				Value(&selected),
		),
	).WithAccessible(initAccessibleMode()).WithTheme(initPromptTheme()).Run(); err != nil {
		return initRequest{}, err
	}

	return initRequest{
		Providers: providers,
		Settings: settings.Settings{
			DefaultJudgeProvider: selected.Provider,
			DefaultJudgeModel:    selected.Model,
		},
	}, nil
}

func runInitRequest(out io.Writer, request initRequest) error {
	if len(request.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	rootDir, err := currentProjectDir()
	if err != nil {
		return err
	}
	bootstrap, err := app.NewBootstrap(rootDir)
	if err != nil {
		return err
	}

	for _, provider := range request.Providers {
		if _, err := bootstrap.InitProvider(provider); err != nil {
			return fmt.Errorf("initialize %s provider: %w", provider, err)
		}
		_, _ = fmt.Fprintf(out, "Installed %s assets.\n", provider.Label())
	}

	saved, err := settings.Save(rootDir, request.Settings)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Default judge: %s / %s\n", saved.DefaultJudgeProvider, saved.DefaultJudgeModel)
	_, _ = fmt.Fprintln(out, "Settings: .reasond/settings.json")
	return nil
}

func judgeChoices() ([]judgeChoice, error) {
	var choices []judgeChoice
	for _, provider := range []assetbundle.Provider{assetbundle.ProviderCodex, assetbundle.ProviderClaude} {
		normalized, err := settings.NormalizeProvider(string(provider))
		if err != nil {
			return nil, err
		}
		models, err := settings.ModelsForProvider(normalized)
		if err != nil {
			return nil, err
		}
		for _, model := range models {
			choices = append(choices, judgeChoice{
				Provider: normalized,
				Model:    model,
			})
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("at least one judge choice is required")
	}
	return choices, nil
}

func judgeChoiceOptions(choices []judgeChoice) []huh.Option[judgeChoice] {
	options := make([]huh.Option[judgeChoice], 0, len(choices))
	for _, choice := range choices {
		options = append(options, huh.NewOption(judgeChoiceLabel(choice), choice))
	}
	return options
}

func providerOptions(selected []assetbundle.Provider) []huh.Option[assetbundle.Provider] {
	return []huh.Option[assetbundle.Provider]{
		huh.NewOption(providerOptionLabel(assetbundle.ProviderCodex), assetbundle.ProviderCodex).Selected(providerSelected(selected, assetbundle.ProviderCodex)),
		huh.NewOption(providerOptionLabel(assetbundle.ProviderClaude), assetbundle.ProviderClaude).Selected(providerSelected(selected, assetbundle.ProviderClaude)),
	}
}

func providerOptionLabel(provider assetbundle.Provider) string {
	return providerLabel(string(provider))
}

func judgeChoiceLabel(choice judgeChoice) string {
	return fmt.Sprintf("%s judge: %s", providerLabel(choice.Provider), choice.Model)
}

func providerLabel(provider string) string {
	switch provider {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	default:
		return provider
	}
}

func providerSelected(selected []assetbundle.Provider, provider assetbundle.Provider) bool {
	for _, candidate := range selected {
		if candidate == provider {
			return true
		}
	}
	return false
}

func initPromptTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		lightDark := lipgloss.LightDark(isDark)
		styles := huh.ThemeCharm(isDark)
		styles.Form.Base = lipgloss.NewStyle().Padding(1, 2)
		styles.Group.Base = lipgloss.NewStyle()
		styles.Group.Title = styles.Focused.Title
		styles.Group.Description = styles.Focused.Description

		// Use Charm-like color without the default heavy left border container.
		styles.Focused.Base = lipgloss.NewStyle().PaddingLeft(0)
		styles.Focused.Card = styles.Focused.Base
		styles.Blurred.Base = lipgloss.NewStyle().PaddingLeft(0)
		styles.Blurred.Card = styles.Blurred.Base

		styles.Focused.SelectedPrefix = lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("#02CF92"), lipgloss.Color("#02A877"))).
			SetString("[x] ")
		styles.Focused.UnselectedPrefix = lipgloss.NewStyle().
			Foreground(lightDark(lipgloss.Color("245"), lipgloss.Color("243"))).
			SetString("[ ] ")
		styles.Blurred.SelectedPrefix = styles.Focused.SelectedPrefix
		styles.Blurred.UnselectedPrefix = styles.Focused.UnselectedPrefix
		styles.Blurred.SelectedOption = styles.Focused.SelectedOption
		styles.Blurred.UnselectedOption = styles.Focused.UnselectedOption
		return styles
	})
}

func initAccessibleMode() bool {
	return os.Getenv("ACCESSIBLE") != ""
}
