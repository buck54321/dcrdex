// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"github.com/rivo/tview"
)

var (
	marketChartWidth  = 90
	marketChartHeight = 10
)

// marketViewer is the market view, which offers the ability to view market
// info and place orders.
type marketViewer struct {
	*tview.Flex
	window *marketWindow
	chain  *focusChain
}

// newMarketView is the constructor for a *marketViewer.
func newMarketView() *marketViewer {
	marketJournal := newJournal("Market Journal", nil)
	marketLog = NewLogger("MRKT", marketJournal.Write)
	marketList := newChooser("Markets", nil)
	markets := clientCore.ListMarkets()
	for _, market := range markets {
		m := market
		marketList.addEntry(market, func() {
			marketLog.Infof("%s selected", m)
		})
	}

	// the marketWindow is the main window on the market view.
	marketWindow := newMarketWindow()

	// The marketColumn holds the marketWindow and the marketJournal.
	marketColumn := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(marketWindow, 0, 3, false).
		AddItem(marketJournal, 0, 1, false)

	wgt := tview.NewFlex().
		AddItem(marketList, 15, 0, false).
		AddItem(marketColumn, 0, 1, false)
	wgt.SetBorder(true).SetBorderColor(blurColor)

	mv := &marketViewer{
		Flex:   wgt,
		window: marketWindow,
	}
	mv.chain = newFocusChain(mv, marketList, marketJournal)

	return mv
}

// AddFocus is part of the focuser interface, and will be called when this
// element receives focus.
func (v *marketViewer) AddFocus() {
	// Pass control to the focusChain when the view receives focus.
	v.chain.focus()
	v.window.chart.Focus(nil)
	v.SetBorderColor(focusColor)
}

// RemoveFocus is part of the focuser interface, and will be called when this
// element loses focus.
func (v *marketViewer) RemoveFocus() {
	v.window.chart.Blur()
	v.SetBorderColor(blurColor)
}

// marketWindow is the market view main window layout manager.
type marketWindow struct {
	*tview.Flex
	chart *depthChart
}

// newMarketWindow is the constructor for a *marketWindow.
func newMarketWindow() *marketWindow {
	chart := newDepthChart()
	chart.SetBorderPadding(1, 1, 1, 1)
	chartBox := tview.NewFlex().
		AddItem(chart, 0, 1, false)
	chartBox.SetBorder(true).SetBorderColor(colorBlack).SetTitle("A Chart")
	wgt := tview.NewFlex().AddItem(fullyCentered(chartBox, marketChartWidth, marketChartHeight+2), 0, 1, false)
	wgt.SetBorder(true).SetBorderColor(colorBlack)
	return &marketWindow{
		Flex:  wgt,
		chart: chart,
	}
}

// AddFocus is part of the focuser interface, and will be called when this
// element receives focus.
func (w *marketWindow) AddFocus() {
	w.SetBorderColor(focusColor)
}

// RemoveFocus is part of the focuser interface, and will be called when this
// element loses focus.
func (w *marketWindow) RemoveFocus() {
	w.SetBorderColor(blurColor)
}
