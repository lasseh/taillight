export interface Theme {
  id: string
  name: string
  isDark: boolean
  chartColors: string[]
}

export const themes: Theme[] = [
  {
    id: 'tokyonight',
    name: 'Tokyo Night',
    isDark: true,
    chartColors: [
      '#7aa2f7', '#9ece6a', '#ff9e64', '#f7768e', '#2ac3de',
      '#bb9af7', '#e0af68', '#ff007c', '#73daca', '#b4f9f8',
    ],
  },
  {
    id: 'tokyonight-storm',
    name: 'Tokyo Night Storm',
    isDark: true,
    chartColors: [
      '#7aa2f7', '#9ece6a', '#ff9e64', '#f7768e', '#2ac3de',
      '#bb9af7', '#e0af68', '#ff007c', '#73daca', '#b4f9f8',
    ],
  },
  {
    id: 'tokyonight-moon',
    name: 'Tokyo Night Moon',
    isDark: true,
    chartColors: [
      '#82aaff', '#c3e88d', '#ff966c', '#ff757f', '#4fd6be',
      '#fca7ea', '#ffc777', '#c099ff', '#86e1fc', '#b4f9f8',
    ],
  },
  {
    id: 'tokyonight-day',
    name: 'Tokyo Night Day',
    isDark: false,
    chartColors: [
      '#2e7de9', '#587539', '#b15c00', '#f52a65', '#118c74',
      '#7847bd', '#8c6c3e', '#9854f1', '#188092', '#d20065',
    ],
  },
  {
    id: 'dracula',
    name: 'Dracula',
    isDark: true,
    chartColors: [
      '#bd93f9', '#50fa7b', '#ffb86c', '#ff5555', '#8be9fd',
      '#ff79c6', '#f1fa8c', '#6272a4', '#69ff94', '#d6acff',
    ],
  },
  {
    id: 'catppuccin',
    name: 'Catppuccin Mocha',
    isDark: true,
    chartColors: [
      '#89b4fa', '#a6e3a1', '#fab387', '#f38ba8', '#94e2d5',
      '#cba6f7', '#f9e2af', '#f5c2e7', '#74c7ec', '#b4befe',
    ],
  },
  {
    id: 'catppuccin-macchiato',
    name: 'Catppuccin Macchiato',
    isDark: true,
    chartColors: [
      '#8aadf4', '#a6da95', '#f5a97f', '#ed8796', '#8bd5ca',
      '#c6a0f6', '#eed49f', '#f5bde6', '#7dc4e4', '#b7bdf8',
    ],
  },
  {
    id: 'catppuccin-frappe',
    name: 'Catppuccin Frappé',
    isDark: true,
    chartColors: [
      '#8caaee', '#a6d189', '#ef9f76', '#e78284', '#81c8be',
      '#ca9ee6', '#e5c890', '#f4b8e4', '#85c1dc', '#babbf1',
    ],
  },
  {
    id: 'catppuccin-latte',
    name: 'Catppuccin Latte',
    isDark: false,
    chartColors: [
      '#1e66f5', '#40a02b', '#fe640b', '#d20f39', '#179299',
      '#8839ef', '#df8e1d', '#ea76cb', '#04a5e5', '#7287fd',
    ],
  },
  {
    id: 'onedark',
    name: 'One Dark',
    isDark: true,
    chartColors: [
      '#61afef', '#98c379', '#d19a66', '#e06c75', '#56b6c2',
      '#c678dd', '#e5c07b', '#be5046', '#7ec8e3', '#a0c980',
    ],
  },
  {
    id: 'solarized',
    name: 'Solarized Dark',
    isDark: true,
    chartColors: [
      '#268bd2', '#859900', '#cb4b16', '#dc322f', '#2aa198',
      '#6c71c4', '#b58900', '#d33682', '#839496', '#93a1a1',
    ],
  },
  {
    id: 'monokai',
    name: 'Monokai',
    isDark: true,
    chartColors: [
      '#66d9ef', '#a6e22e', '#fd971f', '#f92672', '#66d9ef',
      '#ae81ff', '#e6db74', '#f92672', '#a1efe4', '#f8f8f2',
    ],
  },
  {
    id: 'nord',
    name: 'Nord',
    isDark: true,
    chartColors: [
      '#81a1c1', '#a3be8c', '#d08770', '#bf616a', '#88c0d0',
      '#b48ead', '#ebcb8b', '#5e81ac', '#8fbcbb', '#d8dee9',
    ],
  },
  {
    id: 'gruvbox',
    name: 'Gruvbox Dark',
    isDark: true,
    chartColors: [
      '#83a598', '#b8bb26', '#fe8019', '#fb4934', '#8ec07c',
      '#d3869b', '#fabd2f', '#689d6a', '#458588', '#ebdbb2',
    ],
  },
  {
    id: 'rosepine',
    name: 'Rosé Pine',
    isDark: true,
    chartColors: [
      '#c4a7e7', '#9ccfd8', '#f6c177', '#eb6f92', '#31748f',
      '#ebbcba', '#908caa', '#ea9a97', '#3e8fb0', '#e0def4',
    ],
  },
  {
    id: 'synthwave84',
    name: 'SynthWave 84',
    isDark: true,
    chartColors: [
      '#36f9f6', '#72f1b8', '#ff8b39', '#fe4450', '#03edf9',
      '#ff7edb', '#fede5d', '#f97e72', '#2ee2fa', '#b893ce',
    ],
  },
  // Light themes (standalone, not part of a family).
  {
    id: 'light',
    name: 'GitHub Light',
    isDark: false,
    chartColors: [
      '#0550ae', '#116329', '#bc4c00', '#cf222e', '#0969da',
      '#8250df', '#9a6700', '#bf3989', '#1b7c83', '#6639ba',
    ],
  },
  {
    id: 'onelight',
    name: 'Atom One Light',
    isDark: false,
    chartColors: [
      '#4078f2', '#50a14f', '#c18401', '#e45649', '#0184bc',
      '#a626a4', '#986801', '#ca1243', '#0997b3', '#526fff',
    ],
  },
  {
    id: 'winter',
    name: 'Winter',
    isDark: false,
    chartColors: [
      '#2970c7', '#189b2c', '#df8618', '#ef5350', '#4fb4d8',
      '#7c2cbd', '#fa841d', '#dc3eb7', '#00ac8f', '#0991b6',
    ],
  },
]
