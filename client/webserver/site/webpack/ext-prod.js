const configs = require('./configs.js')
const { merge } = require('webpack-merge')

module.exports = merge(configs.common, configs.extConfig, configs.productionConfig)
