chrome.tabs.onUpdated.addListener(async (tabID, changeInfo, tab) => {
  if (tab.url.startsWith('chrome:')) return
  if (changeInfo.status !== 'loading') return

  chrome.scripting.insertCSS({
    target: { tabId: tabID },
    files: ["/cdist/appconnector.css"]
});

  chrome.scripting.executeScript({
    target: { tabId: tabID },
    files: ['cdist/appconnector.js'],
    injectImmediately: true
  })
})
