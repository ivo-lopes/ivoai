import React from 'react';
import MDXComponents from '@theme-original/MDXComponents';

function AccessibleTable(props: React.TableHTMLAttributes<HTMLTableElement>): React.JSX.Element {
  return (
    <div className="ivoai-table-region" role="region" aria-label="Scrollable table" tabIndex={0}>
      <table {...props} />
    </div>
  );
}

export default {
  ...MDXComponents,
  table: AccessibleTable,
};
