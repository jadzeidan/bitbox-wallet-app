// SPDX-License-Identifier: Apache-2.0

import { useState, ReactNode } from 'react';
import { A } from '@/components/anchor/anchor';
import style from './guide.module.css';

export type TEntryProp = {
  title: string;
  text: string;
  link?: {
    url?: string;
    text: string;
  };
};

type TEntryProps = {
  entry: TEntryProp;
  shown?: boolean;
  children?: ReactNode;
};

type TProps = TEntryProps;

const renderText = (text: string) => {
  const content: ReactNode[] = [];
  let listItems: string[] = [];

  const flushList = () => {
    if (!listItems.length) {
      return;
    }

    content.push(
      <ul className={style.list} key={`list-${content.length}`}>
        {listItems.map((item, index) => (
          <li key={index}>{item}</li>
        ))}
      </ul>
    );
    listItems = [];
  };

  text.trim().split('\n').forEach(line => {
    const trimmedLine = line.trim();

    if (!trimmedLine) {
      flushList();
      return;
    }

    if (trimmedLine.startsWith('- ')) {
      listItems.push(trimmedLine.slice(2));
      return;
    }

    flushList();
    content.push(<p key={`paragraph-${content.length}`}>{trimmedLine}</p>);
  });

  flushList();

  return content;
};

export const Entry = (props: TProps) => {
  const [shown, setShown] = useState<boolean>(props.shown || false);

  const toggle = () => {
    setShown(shown => !shown);
  };

  const entry = props.entry;
  return (
    <div className={style.entry}>
      <div className={style.entryTitle} onClick={toggle}>
        <div className={style.entryToggle}>
          {shown ? '–' : '+'}
        </div>
        <h3 className={style.entryTitleText}>
          {entry.title}
        </h3>
      </div>
      <div className={[style.entryContent, shown ? style.expanded : ''].join(' ')}>
        {shown ? (
          <div className="flex-1">
            {renderText(entry.text)}
            {entry.link && (
              <p>
                {entry.link.url ? (
                  <A
                    className={style.link}
                    data-testid="link"
                    href={entry.link.url}>
                    {entry.link.text}
                  </A>
                ) : (
                  <span className={style.link}>
                    {entry.link.text}
                  </span>
                )}
              </p>
            )}
            {props.children}
          </div>
        ) : null}
      </div>
    </div>
  );
};
