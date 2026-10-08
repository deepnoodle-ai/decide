// @ts-check
import { defineEcConfig, ExpressiveCodeTheme } from '@astrojs/starlight/expressive-code';
import { pluginCollapsibleSections } from '@expressive-code/plugin-collapsible-sections';
import codeTheme from './src/styles/code-theme.json' with { type: 'json' };

// One dark theme in both site themes: code blocks match the recordings.
const theme = ExpressiveCodeTheme.fromJSONString(JSON.stringify(codeTheme));

export default defineEcConfig({
	themes: [theme],
	// collapse={…} folds the boilerplate in a long block, such as the
	// install step of a workflow. Long lines wrap rather than scroll.
	plugins: [pluginCollapsibleSections()],
	defaultProps: { wrap: true },
	useStarlightDarkModeSwitch: false,
	useStarlightUiThemeColors: false,
	styleOverrides: {
		borderRadius: '12px',
		borderColor: 'var(--term-border)',
		codeFontFamily: 'var(--sl-font-mono)',
		codeFontSize: '13.5px',
		codeLineHeight: '1.6',
		codePaddingInline: '20px',
		uiFontFamily: 'var(--sl-font)',
		collapsibleSections: {
			closedBackgroundColor: 'rgba(255, 255, 255, 0.04)',
			closedBorderColor: 'rgba(255, 255, 255, 0.06)',
			closedTextColor: '#A4A8AF',
			openBackgroundColorCollapsible: 'rgba(255, 255, 255, 0.03)',
		},
		frames: {
			shadowColor: 'transparent',
			frameBoxShadowCssValue: 'var(--term-shadow)',
			terminalBackground: '#121316',
			terminalTitlebarBackground: '#121316',
			terminalTitlebarForeground: '#A4A8AF',
			terminalTitlebarBorderBottomColor: 'rgba(255, 255, 255, 0.06)',
			terminalTitlebarDotsOpacity: '0',
			editorTabBarBackground: '#121316',
			editorActiveTabBackground: '#121316',
			editorActiveTabForeground: '#A4A8AF',
			editorTabBarBorderBottomColor: 'rgba(255, 255, 255, 0.06)',
			editorActiveTabIndicatorTopColor: 'transparent',
			editorActiveTabIndicatorBottomColor: 'transparent',
			inlineButtonForeground: '#A4A8AF',
			inlineButtonBorder: 'rgba(255, 255, 255, 0.12)',
		},
	},
});
