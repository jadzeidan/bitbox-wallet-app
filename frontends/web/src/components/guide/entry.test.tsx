// SPDX-License-Identifier: Apache-2.0

import '../../../__mocks__/i18n';
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Entry, TEntryProp } from './entry';
vi.mock('@/i18n/i18n');

vi.mock('@/utils/request', () => ({
  apiGet: vi.fn().mockResolvedValue(''),
}));

describe('components/guide/entry', () => {
  describe('renders correct entry values', () => {
    it('collapsed', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: 'Some text',
      };
      const { container } = render(<Entry key={'key'} entry={EntryProps} />);

      expect(container).toHaveTextContent('A title');
      expect(container).not.toHaveTextContent('Some text');
    });

    it('opened', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: 'Some text',
      };
      const { container } = render(<Entry key={'key'} entry={EntryProps} shown />);

      expect(container).toHaveTextContent('A title');
      expect(container).toHaveTextContent('Some text');
    });

    it('collapsed & has a link ', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: 'Some text',
        link: {
          url: 'http://someurl.com',
          text: 'some url'
        }
      };
      const { container, queryByTestId } = render(<Entry key={'key'} entry={EntryProps} />);

      const link = queryByTestId('link');

      expect(container).toHaveTextContent('A title');
      expect(container).not.toHaveTextContent('Some text');
      expect(container).not.toHaveTextContent('some url');
      expect(link).toBeNull();
    });

    it('opened & has a link ', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: 'Some text',
        link: {
          url: 'http://someurl.com',
          text: 'some url'
        }
      };
      const { container, getByTestId } = render(<Entry key={'key'} entry={EntryProps} shown />);

      const link = getByTestId('link');

      expect(container).toHaveTextContent('A title');
      expect(container).toHaveTextContent('Some text');
      expect(container).toHaveTextContent('some url');
      expect(link).toBeInTheDocument();
      expect(link).toHaveAttribute('title', 'http://someurl.com');
    });

    it('opened & has bullet-only content', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: '- Value over time\n- Performance',
      };
      const { container } = render(<Entry key={'key'} entry={EntryProps} shown />);

      expect(container.querySelectorAll('ul')).toHaveLength(1);
      expect(container.querySelectorAll('li')).toHaveLength(2);
      expect(screen.getByText('Value over time')).toBeInTheDocument();
      expect(screen.getByText('Performance')).toBeInTheDocument();
    });

    it('opened & switches between paragraphs and lists', () => {
      const EntryProps: TEntryProp = {
        title: 'A title',
        text: 'Intro paragraph\n\n- First item\n- Second item\n\nClosing paragraph',
      };
      const { container } = render(<Entry key={'key'} entry={EntryProps} shown />);

      expect(container.querySelectorAll('p')).toHaveLength(2);
      expect(container.querySelectorAll('ul')).toHaveLength(1);
      expect(container.querySelectorAll('li')).toHaveLength(2);
      expect(screen.getByText('Intro paragraph')).toBeInTheDocument();
      expect(screen.getByText('First item')).toBeInTheDocument();
      expect(screen.getByText('Second item')).toBeInTheDocument();
      expect(screen.getByText('Closing paragraph')).toBeInTheDocument();
    });
  });
});
