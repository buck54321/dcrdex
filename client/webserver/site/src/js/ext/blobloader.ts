import { Chart, Extents, Region, Point, Translator } from '../charts'
import { Animation } from '../doc'

export class BlobLoader extends Chart {
  ani: Animation

  constructor (parent: HTMLElement) {
    super(parent, {
      resize: () => this.resized(),
      click: (/* e: MouseEvent */) => { /* pass */ },
      zoom: (/* bigger: boolean */) => { /* pass */ }
    })
    this.canvas.classList.add('fill-abs')
    this.canvas.style.zIndex = '5'

    this.resize()
    this.ani = new Animation(Animation.Forever, () => {
      this.render()
    })
  }

  resized () {
    /* do stuff */
    let { width: w, height: h } = this.canvas
    let [x, y] = [0, 0]
    const aspectRatio = 1 / 2
    if (w / h > aspectRatio) {
      w = h * aspectRatio
      x = (this.canvas.width - w) / 2
    } else {
      h = w / aspectRatio
      y = (this.canvas.height - h) / 2
    }
    this.plotRegion = new Region(this.ctx, new Extents(x, this.canvas.width - x, y, this.canvas.height - y))
    this.render()
  }

  render () {
    if (!this.visible || this.canvas.width === 0) {
      this.renderScheduled = true
      return
    }
    this.clear()
    this.ctx.fillStyle = window.getComputedStyle(document.body, null).getPropertyValue('background-color')
    this.ctx.fillRect(0, 0, this.canvas.width, this.canvas.height)

    this.ctx.fillStyle = '#777'
    this.ctx.lineWidth = 2
    const period = 3000 // milliseconds
    const theta = new Date().getTime() / period * Math.PI * 2
    const powFactor = 2
    const topBottomPhase = Math.PI / 18
    const topY = 0.35 + Math.pow(Math.cos(theta + topBottomPhase), powFactor) * 0.5 // y: 0.85 -> 0.35, range: 0.5
    const bottomY = 0.15 + Math.pow(Math.cos(theta), powFactor) * 0.5 // y: 0.65 -> 0.15, median: 0.4
    const topOffsetX = 0.2 + Math.pow(Math.sin(theta + topBottomPhase), 2) * 0.2 // 0.2 -> 0.4, range 0.2
    const bottomOffsetX = 0.2 + Math.pow(Math.cos(theta), powFactor) * 0.2

    const plotPoints = (ctx: CanvasRenderingContext2D, pts: Point[]) => {
      const n = pts.length
      ctx.beginPath()
      ctx.moveTo(pts[0].x, pts[0].y)
      for (let i = 0; i < 4; i++) {
        const ptBefore = pts[(i + n - 1) % n]
        const pt1 = pts[i]
        const pt2 = pts[(i + 1) % n]
        const ptAfter = pts[(i + 2) % n]
        const [handle1, handle2] = controlPoints(ptBefore, pt1, pt2, ptAfter)
        ctx.bezierCurveTo(handle1.x, handle1.y, handle2.x, handle2.y, pt2.x, pt2.y)
      }
      ctx.fill()
    }

    this.plotRegion.plot(new Extents(0, 1, 0, 1), (ctx: CanvasRenderingContext2D, tools: Translator) => {
      const pt0 = new Point(tools.x(topOffsetX), tools.y(topY))
      const pt1 = new Point(tools.x(1 - topOffsetX), tools.y(topY))
      const pt2 = new Point(tools.x(1 - bottomOffsetX), tools.y(bottomY))
      const pt3 = new Point(tools.x(bottomOffsetX), tools.y(bottomY))
      plotPoints(ctx, [pt0, pt1, pt2, pt3])
    })
  }

  stop () {
    this.ani.stop()
    this.canvas.remove()
  }
}

const SmoothingFactor = 1

// https://stackoverflow.com/questions/15691499/how-do-i-draw-a-closed-curve-over-a-set-of-points
function controlPoints (ptBefore: Point, pt1: Point, pt2: Point, ptAfter: Point): [Point, Point] {
  const xc1 = (ptBefore.x + pt1.x) / 2
  const yc1 = (ptBefore.y + pt1.y) / 2
  const xc2 = (pt1.x + pt2.x) / 2
  const yc2 = (pt1.y + pt2.y) / 2
  const xc3 = (pt2.x + ptAfter.x) / 2
  const yc3 = (pt2.y + ptAfter.y) / 2

  const len1 = Math.sqrt((pt1.x - ptBefore.x) * (pt1.x - ptBefore.x) + (pt1.y - ptBefore.y) * (pt1.y - ptBefore.y))
  const len2 = Math.sqrt((pt2.x - pt1.x) * (pt2.x - pt1.x) + (pt2.y - pt1.y) * (pt2.y - pt1.y))
  const len3 = Math.sqrt((ptAfter.x - pt2.x) * (ptAfter.x - pt2.x) + (ptAfter.y - pt2.y) * (ptAfter.y - pt2.y))

  const k1 = len1 / (len1 + len2)
  const k2 = len2 / (len2 + len3)

  const xm1 = xc1 + (xc2 - xc1) * k1
  const ym1 = yc1 + (yc2 - yc1) * k1

  const xm2 = xc2 + (xc3 - xc2) * k2
  const ym2 = yc2 + (yc3 - yc2) * k2

  const handle1X = xm1 + (xc2 - xm1) * SmoothingFactor + pt1.x - xm1
  const handle1Y = ym1 + (yc2 - ym1) * SmoothingFactor + pt1.y - ym1

  const handle2X = xm2 + (xc2 - xm2) * SmoothingFactor + pt2.x - xm2
  const handle2Y = ym2 + (yc2 - ym2) * SmoothingFactor + pt2.y - ym2

  return [new Point(handle1X, handle1Y), new Point(handle2X, handle2Y)]
}
