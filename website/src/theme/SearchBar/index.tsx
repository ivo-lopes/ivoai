import React, {useEffect} from 'react';
import {translate} from '@docusaurus/Translate';
import SearchBar from '@theme-original/SearchBar';

type Props = React.ComponentProps<typeof SearchBar>;

export default function LocalizedSearchBar(props: Props): React.JSX.Element {
  const label = translate({
    id: 'theme.SearchBar.label',
    message: 'Search',
    description: 'The ARIA label and placeholder for the search button',
  });

  useEffect(() => {
    const localizeAccessibleName = (): void => {
      document.querySelectorAll<HTMLInputElement>('.navbar__search-input').forEach((input) => {
        if (input.getAttribute('aria-label') !== label) {
          input.setAttribute('aria-label', label);
        }
      });
    };

    localizeAccessibleName();
    const observer = new MutationObserver(localizeAccessibleName);
    observer.observe(document.body, {
      attributes: true,
      attributeFilter: ['aria-label'],
      childList: true,
      subtree: true,
    });
    return () => observer.disconnect();
  }, [label]);

  return <SearchBar {...props} />;
}
