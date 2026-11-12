// The home page's link preview: the hero as a card, with the character
// over "Judgment calls, at scale." Everything sits in the middle, so a chat
// app that crops the card to a square, as Slack does, still shows all of it.
// Other pages' cards come from [...route].ts.
import type { APIRoute } from 'astro';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import init from 'canvaskit-wasm/full';

const [W, H] = [1200, 630];
const bg = [18, 19, 22];
const white = [243, 244, 245];
const dim = [126, 130, 138]; // --sl-color-gray-3, as the hero's "at scale."
const grey = [180, 183, 189]; // --sl-color-gray-2
const hairline = [46, 48, 54];

// The ask face from Doodle.astro, in its 36-unit view box (-2 to 34).
const head =
	'M18.59 4.84C19.43 5.39 22.46 7 23.61 8.14C24.76 9.27 25.21 9.98 25.5 11.63C25.79 13.27 25.8 16.2 25.35 18.01C24.9 19.83 23.96 21.49 22.8 22.52C21.65 23.55 20.14 23.96 18.43 24.21C16.72 24.46 14.31 24.47 12.55 24.01C10.79 23.54 9.02 22.67 7.86 21.43C6.69 20.18 5.81 18.36 5.56 16.55C5.32 14.73 5.74 12.19 6.39 10.55C7.03 8.9 8.35 7.72 9.42 6.67C10.48 5.61 12.22 4.64 12.78 4.23';
const brow = 'M16.6 7C17.5 6.7 19.4 6.75 20.3 7.15C21.1 7.5 21.6 8.1 21.9 8.4';
const mouth = 'M9.8 18.35C10.9 18.8 12.6 19 14.3 18.7';
const eyes = [
	[11.7, 11.2, 2.15, 2.3],
	[19.8, 10.75, 1.35, 1.6],
];
const dots = [
	[26.2, 8.2, 0.85],
	[28.1, 5.8, 1.1],
	[29.8, 3.4, 1.35],
];

export const GET: APIRoute = async () => {
	const require = createRequire(import.meta.url);
	const CK = await init({ locateFile: (file) => require.resolve(`canvaskit-wasm/bin/full/${file}`) });
	const font = (weight: number) => readFile(`./node_modules/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-${weight}-normal.woff`);
	const fonts = CK.FontMgr.FromData(...(await Promise.all([font(400), font(500)])).map((b) => b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength)));
	if (!fonts) throw new Error('og/index.png: the JetBrains Mono fonts did not load');

	const surface = CK.MakeSurface(W, H);
	if (!surface) throw new Error('og/index.png: no canvas surface');
	const canvas = surface.getCanvas();
	const color = ([r, g, b]: number[]) => CK.Color(r, g, b, 1);
	canvas.clear(color(bg));

	const text = (lines: [string, number[]][], size: number, weight: number, lineHeight: number) => {
		const style = (c: number[]) => ({
			color: color(c),
			fontFamilies: ['JetBrains Mono'],
			fontSize: size,
			fontStyle: { weight: { value: weight } },
			heightMultiplier: lineHeight,
		});
		const pb = CK.ParagraphBuilder.Make(new CK.ParagraphStyle({ textStyle: style(lines[0][1]), textAlign: CK.TextAlign.Center }), fonts);
		lines.forEach(([s, c], i) => {
			pb.pushStyle(new CK.TextStyle(style(c)));
			pb.addText(i ? `\n${s}` : s);
			pb.pop();
		});
		const p = pb.build();
		p.layout(W);
		return p;
	};
	const title = text([['Judgment calls,', white], ['at scale.', dim]], 64, 500, 1.1);
	const site = text([['decide.deepnoodle.ai', grey]], 26, 400, 1.2);

	// Stack the face, the title and the site, centered as a block.
	const face = 240;
	const scale = face / 36;
	// The head ends well above its view box, so the first gap is small.
	const [gap1, gap2] = [0, 40];
	const total = face + gap1 + title.getHeight() + gap2 + site.getHeight();
	let y = (H - total) / 2;

	// The face: rotated -9° about its center, as in Doodle.astro.
	canvas.save();
	canvas.translate(W / 2 - (16 * scale), y + 2 * scale);
	canvas.scale(scale, scale);
	canvas.rotate(-9, 15.6, 14.4);
	const pen = new CK.Paint();
	pen.setAntiAlias(true);
	pen.setColor(color(white));
	pen.setStyle(CK.PaintStyle.Stroke);
	pen.setStrokeCap(CK.StrokeCap.Round);
	pen.setStrokeJoin(CK.StrokeJoin.Round);
	const fill = pen.copy();
	fill.setStyle(CK.PaintStyle.Fill);
	const stroke = (d: string, width: number) => {
		const path = CK.Path.MakeFromSVGString(d);
		if (!path) throw new Error('og/index.png: a doodle path did not parse');
		pen.setStrokeWidth(width / scale);
		canvas.drawPath(path, pen);
		path.delete();
	};
	stroke(head, 6);
	stroke(brow, 5);
	stroke(mouth, 5);
	for (const [cx, cy, rx, ry] of eyes) canvas.drawOval(CK.LTRBRect(cx - rx, cy - ry, cx + rx, cy + ry), fill);
	for (const [cx, cy, r] of dots) canvas.drawCircle(cx, cy, r, fill);
	canvas.restore();
	y += face + gap1;

	canvas.drawParagraph(title, 0, y);
	y += title.getHeight() + gap2;
	canvas.drawParagraph(site, 0, y);

	// The same edge as the other pages' cards.
	const edge = new CK.Paint();
	edge.setColor(color(hairline));
	canvas.drawRect(CK.LTRBRect(0, H - 2, W, H), edge);

	const image = surface.makeImageSnapshot();
	const png = image.encodeToBytes();
	if (!png) throw new Error('og/index.png: the image did not encode');
	return new Response(png, { headers: { 'Content-Type': 'image/png' } });
};
