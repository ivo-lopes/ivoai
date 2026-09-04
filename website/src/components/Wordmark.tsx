import React from 'react';
import clsx from 'clsx';

type Props = {
  className?: string;
};

// Bpmf Huninn does not contain a Greek capital lambda. Keep the surrounding
// lettering in the requested face and draw the brand lambda as two rounded
// vector strokes so the wordmark never falls back to an unrelated system font.
export default function Wordmark({className}: Props): React.JSX.Element {
  return (
    <span className={clsx('ivoai-wordmark', className)} aria-hidden="true">
      <span>I</span>
      <span>V</span>
      <span>O</span>
      <svg
        className="ivoai-wordmark__lambda"
        viewBox="0 0 20 24"
        focusable="false"
        aria-hidden="true">
        <path d="M2 22 L10 2 L18 22" />
      </svg>
      <span>I</span>
    </span>
  );
}
