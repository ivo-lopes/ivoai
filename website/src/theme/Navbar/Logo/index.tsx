import React from 'react';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Wordmark from '@site/src/components/Wordmark';

export default function NavbarLogo(): React.JSX.Element {
  return (
    <Link className="navbar__brand" to={useBaseUrl('/')} aria-label="IVOAI home">
      <Wordmark className="navbar__title" />
    </Link>
  );
}
