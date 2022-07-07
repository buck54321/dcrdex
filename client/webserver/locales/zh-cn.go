package locales

var ZhCN = map[string]string{
	"Language":                       "zh-CN",
	"Markets":                        "市场",
	"Wallets":                        "钱包",
	"Notifications":                  "通知",
	"Recent Activity":                "近期活动",
	"Sign Out":                       "退出",
	"Order History":                  "历史订单",
	"load from file":                 "从文件加载",
	"loaded from file":               "从文件加载",
	"defaults":                       "默认",
	"Wallet Password":                "钱包密码",
	"w_password_helper":              "这是您用钱包中的软件配置的密码。",
	"w_password_tooltip":             "如果您的钱包不需要密码，请将密码清空。",
	"App Password":                   "应用程序密码",
	"app_password_helper":            "执行敏感投资操作时，需要输入您的应用程序密码。",
	"Add":                            "加",
	"Unlock":                         "解锁",
	"Wallet":                         "钱包",
	"app_password_reminder":          "执行敏感投资操作时，需要输入您的应用程序密码",
	"DEX Address":                    "DEX 地址",
	"TLS Certificate":                "TLS 证书",
	"remove":                         "去除",
	"add a file":                     "添加文件",
	"Submit":                         "提交",
	"Confirm Registration":           "确认注册",
	"app_pw_reg":                     "输入应用程序密码以确认注册DEX。",
	"reg_confirm_submit":             `当您发送此表格时, <span id="feeDisplay"></span> DCR将从您指定的钱包中支出，以支付注册费。`, // update
	"provided_markets":               "当前DEX提供以下市场:",                                                   // alt. DEX提供以下市场
	"accepted_fee_assets":            "This DEX accepts the following fees:",                           // xxx translate
	"base_header":                    "Base",                                                           // no good translation
	"quote_header":                   "Quote",                                                          // no good translation
	"lot_size_header":                "批量",
	"lot_size_headsup":               "所有交易都是批量值的倍数。",
	"Password":                       "密码",
	"Register":                       "注册",
	"Authorize Export":               "授权转出",
	"export_app_pw_msg":              "输入密码以确认帐户导出",
	"Disable Account":                "禁用帐户",
	"disable_app_pw_msg":             "输入您的密码以禁用帐户",
	"disable_dex_server":             "该 DEX 服务器可能会在未来任何时候通过再次添加重新启用（您无需支付费用）。", // RETRANSLATE
	"Authorize Import":               "允许导入",
	"app_pw_import_msg":              "输入应用密码以确认帐户的导入",
	"Account File":                   "帐户文件",
	"Change Application Password":    "更改应用密码",
	"Current Password":               "当前密码",
	"New Password":                   "新密码",
	"Confirm New Password":           "确认新密码",
	"Cancel Order":                   "取消请求",         // alt. 取消订单
	"cancel_pw":                      "输入密码以取消剩余的请求", // alt. 输入密码以取消订单未成交的
	"cancel_no_pw":                   "取消剩余未成交订单",
	"cancel_remain":                  "剩余数量可在取消请求一致之前更改。",
	"Log In":                         "登录",
	"epoch":                          "时间",
	"price":                          "价格",
	"volume":                         "量",
	"buys":                           "买", // same as "Buy" translation
	"sells":                          "卖", // same as "Sell" translation
	"Buy Orders":                     "买单",
	"Quantity":                       "数量",
	"Rate":                           "比率",
	"Epoch":                          "时间", // same as above, just capitalized
	"Limit Order":                    "限价单",
	"Market Order":                   "市价单",
	"reg_status_msg":                 `为了切换到 <span id="regStatusDex" class="text-break"></span>，必须支付注册费 <span id="confReq"></span> 确认书。`,
	"Buy":                            "买",
	"Sell":                           "卖",
	"Lot Size":                       "批量",
	"Rate Step":                      "兑换", // alt. 兑换通行证
	"Max":                            "最多", // "max buy" alt 最多买入
	"lot":                            "批处理",
	"Price":                          "价格",
	"Lots":                           "批",
	"min trade is about":             "最小交换结束了",
	"immediate_explanation":          "如果订单在下一个周期中没有完成，任何剩余的金额将不会在下一个周期中保留或重新组合。",
	"Immediate or cancel":            "立即或取消",
	"Balances":                       "余额",
	"outdated_tooltip":               "账户可能已过期。请连接到钱包进行更新。",
	"available":                      "可用",
	"connect_refresh_tooltip":        "点击连接并刷新",
	"add_a_base_wallet":              `添加一个<br><span data-unit="base"></span><br>钱包`,
	"add_a_quote_wallet":             `添加一个<br><span data-unit="quote"></span><br>钱包`,
	"locked":                         "锁定",
	"immature":                       "不成熟",
	"Sell Orders":                    "卖单",
	"Your Orders":                    "你的订单",
	"Type":                           "类型",
	"Side":                           "侧面",
	"Age":                            "年龄",
	"Filled":                         "已满",
	"Settled":                        "稳定的",
	"Status":                         "状态",
	"view order history":             "查看历史订单",
	"cancel order":                   "取消订单",
	"order details":                  "订单详情",
	"verify_order":                   `检查<span id="vSideHeader"></span> 请求`, // "verify buy order" 检查买单
	"You are submitting an order to": "您正在提交一下订单",                           // alt. 您正在提交订单至
	"at a rate of":                   "汇率",
	"for a total of":                 "共计", // alt. 总共
	"verify_market":                  "这是一个市价单，将匹配订单簿中的最佳订单。根据当前的平均市场价，您将收到",
	"auth_order_app_pw":              "使用应用密码授权此订单。",
	"lots":                           "批", // alt. 很多
	"order_disclaimer": `<span class="red">交易需要</span>一定时间才能完成，禁止关闭 DEX 客户端软件以及
		<span data-unit="quote"></span>或<span data-unit="base"></span>钱包软件。
		交易可能在几分钟内完成，也可能长至几个小时。`,
	"Order":                     "订单",
	"see all orders":            "查看所有订单",
	"Exchange":                  "交易",
	"Market":                    "市场",
	"Offering":                  "供",
	"Asking":                    "问",
	"Fees":                      "费用",
	"order_fees_tooltip":        "区块链交易费用，通常由矿工收取。Decred DEX 不收取交易费用。",
	"Matches":                   "组合",
	"Match ID":                  "匹配ID",
	"Time":                      "时间",
	"ago":                       "前",
	"Cancellation":              "取消",
	"Order Portion":             "订单部分",
	"Swap":                      "", // 各方投出的第一笔链上交易的标签
	"you":                       "你",
	"them":                      "他们",
	"Redemption":                "赎回",
	"Refund":                    "退款",
	"Funding Coins":             "资金硬币",
	"Exchanges":                 "交易市场",
	"apply":                     "申请",
	"Assets":                    "资产",
	"Trade":                     "贸易",
	"Set App Password":          "设置应用密码",
	"reg_set_app_pw_msg":        "设置应用密码。这个密码将保护你的 DEX 账户和连接的密钥和钱包。",
	"Password Again":            "再次输入密码",
	"Add a DEX":                 "添加一个 DEX",
	"reg_ssl_needed":            "我们似乎没有此 DEX 的 SSL 证书。添加服务器证书以便我们可以继续。",
	"Dark Mode":                 "暗模式",
	"Show pop-up notifications": "显示弹出通知",
	"Account ID":                "帐户 ID",
	"Export Account":            "账户导出",
	"simultaneous_servers_msg":  "DEX 客户端支持同时使用多个 DEX 服务器。",
	"Change App Password":       "更改应用密码",
	"Build ID":                  "创建ID",
	"Connect":                   "连接",
	"Withdraw":                  "提款",
	"Deposit":                   "存款",
	"Lock":                      "锁定",
	"New Deposit Address":       "新存款地址",
	"Address":                   "地址",
	"Amount":                    "金额",
	"Reconfigure":               "重新配置",
	"pw_change_instructions":    "更改下面的密码不会更改您钱包中的密码。在您直接通过电子钱包应用程序更改电子钱包密码后，使用此表单更新DEX客户端。",
	"New Wallet Password":       "新钱包密码",
	"pw_change_warn":            "注意：在有活跃交易或预订订单时切换到不同的钱包可能会导致资金丢失。",
	"Show more options":         "显示更多选项",
	"seed_implore_msg":          "请注意。您需要写下种子并保存副本。如果您无法访问此计算机或发生任何其它问题，您可以使用该种子反向访问您的DEX帐户和注册钱包。请将帐户备份与种子分开保存。",
	"View Application Seed":     "查看应用种子",
	"Remember my password":      "记住我的密码",
	"pw_for_seed":               "输入您的应用密码以显示您的种子。确保没有其它人可以看到您的屏幕。",
	"Asset":                     "列表", // alt. 活动
	"Balance":                   "余额", // alt. 平衡
	"Actions":                   "操作", // alt. 动作
	"Restoration Seed":          "恢复种子",
	"Restore from seed":         "从种子恢复",
	"Import Account":            "导入帐户",
	"create_a_x_wallet":         "创建<span data-asset-name=1></span>钱包",
	"dont_share":                "请好好保存切勿分享它。",
	"Show Me":                   "展示",
	"Wallet Settings":           "钱包设置",
	"add_a_x_wallet":            `添加<img data-tmpl="assetLogo" class="asset-logo mx-1"> <span data-tmpl="assetName"></span>钱包`,
	"Export Trades":             "导出交易",
	"Create":                    "创建",
	"Register_loudly":           "注册!",
}
