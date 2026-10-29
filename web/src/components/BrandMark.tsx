/**
 * The onlet mark: a loop with one lit segment, work going round the line. The loop takes the
 * surrounding text colour, so it reads on the charcoal rail and on a light card alike. The
 * geometry matches web/public/icon.svg, cropped to the loop.
 */
export function BrandMark() {
  return (
    <svg className="brand-mark" viewBox="106 106 300 300" aria-hidden="true" focusable="false">
      <path
        d="M383.5 244.8 A128 128 0 1 1 267.2 128.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="44"
        strokeLinecap="round"
      />
      <path
        d="M329.4 151.1 A128 128 0 0 1 360.9 182.6"
        fill="none"
        stroke="var(--lamp-call)"
        strokeWidth="44"
        strokeLinecap="round"
      />
    </svg>
  );
}
