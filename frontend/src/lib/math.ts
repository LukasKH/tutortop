/** v kept between lo and hi. When the range is empty (hi < lo), lo wins. */
export const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), Math.max(lo, hi));
