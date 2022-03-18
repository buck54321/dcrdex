const { merge } = require('webpack-merge')
const common = require('./common.js')

module.exports = merge(common, {
  mode: 'development',
  module: {
    rules: [{
      test: /\.tsx?$/,
      use: 'ts-loader',
      exclude: /node_modules/,
    }]
  },
  devtool: 'inline-source-map'
})
