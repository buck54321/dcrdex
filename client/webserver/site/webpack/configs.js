const webpack = require('webpack')
const { CleanWebpackPlugin } = require('clean-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const StyleLintPlugin = require('stylelint-webpack-plugin')
const ESLintPlugin = require('eslint-webpack-plugin')
const childProcess = require('child_process')
const CssMinimizerPlugin = require('css-minimizer-webpack-plugin')
const path = require('path')

function git (command) {
  return childProcess.execSync(`git ${command}`, { encoding: 'utf8' }).trim()
}

function newPlugins (cssPath) {
  return [
    new webpack.EnvironmentPlugin({
      COMMITHASH: git('rev-parse HEAD')
    }),
    new CleanWebpackPlugin(),
    new MiniCssExtractPlugin({
      // This determines where the css file goes and what it's name is.
      // The root folder will be deleted.
      // But where is the root folder set?
      filename: cssPath
    }),
    new StyleLintPlugin({
      threads: true
    }),
    new ESLintPlugin({
      extensions: ['ts'],
      formatter: 'stylish'
    })
  ]
}

module.exports = {
  common: {
    target: 'web',
    module: {
      rules: [
        {
          test: /\.s?[ac]ss$/,
          use: [
            MiniCssExtractPlugin.loader,
            {
              loader: 'css-loader',
              options: {
                modules: false,
                url: false,
                sourceMap: true
              }
            },
            {
              loader: 'sass-loader',
              options: {
                implementation: require('sass'), // dart-sass
                sourceMap: true
              }
            }
          ]
        }
      ]
    },
    resolve: {
      extensions: ['.ts', '.js']
    },
    // Fixes weird issue with watch script. See
    // https://github.com/webpack/webpack/issues/2297#issuecomment-289291324
    watchOptions: {
      poll: true
    }
  },

  dexcConfig: {
    name: 'dexc',
    entry: path.resolve(__dirname, '../src/index.ts'),
    output: {
      filename: 'entry.js',
      path: path.resolve(__dirname, '../dist'),
      publicPath: '/dist/'
    },
    plugins: newPlugins('style.css')
  },

  extConfig: {
    name: 'ext',
    entry: path.resolve(__dirname, '../src/ext-entry.ts'),
    output: {
      filename: 'dexc.js',
      path: path.resolve(__dirname, '../ext/dist'),
      publicPath: '/dist/'
    },
    plugins: newPlugins('dexc.css')
  },

  connectorConfig: {
    name: 'appconnector',
    entry: path.resolve(__dirname, '../src/appconnector-entry.ts'),
    output: {
      filename: 'appconnector.js',
      path: path.resolve(__dirname, '../ext/cdist'),
      publicPath: '/dist/'
    },
    plugins: newPlugins('appconnector.css')
  },

  devConfig: {
    mode: 'development',
    module: {
      rules: [{
        test: /\.ts$/,
        use: 'ts-loader',
        exclude: /node_modules/
      }]
    },
    devtool: 'inline-source-map'
  },

  productionConfig: {
    mode: 'production',
    // https://stackoverflow.com/questions/48047150/chrome-extension-compiled-by-webpack-throws-unsafe-eval-error
    devtool: 'source-map',
    optimization: {
      usedExports: true,
      minimize: true,
      minimizer: [
        '...', // extend webpack 5's TerserPlugin
        new CssMinimizerPlugin({})
      ]
    },
    module: {
      rules: [
        {
          test: /\.ts$/,
          exclude: /node_modules/,
          use: {
            // babel-loader does not fail on type errors. ts-loader does, but we
            // probably still want to transpile (right?).
            loader: 'babel-loader',
            options: {
              presets: [
                [
                  '@babel/preset-typescript',
                  {
                    exclude: ['@babel/plugin-transform-typescript']
                  }
                ]
              ]
            }
          }
        }
      ]
    }
  }
}
