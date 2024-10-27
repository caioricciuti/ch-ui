import { Meta, StoryObj } from '@storybook/react';
import CHUItable from './CHUItable';
import StoryBookDecorator from '@/storybook/StoryBookDecorator';
import { tableData, tableMeta } from '@/storybook/mocks/table';

const meta: Meta<typeof CHUItable> = {
  title: 'CHUItable',
  decorators: [
    (Story) => StoryBookDecorator({ children: <Story /> }),
  ],
  component: CHUItable,
}

export default meta;
type Story = StoryObj<typeof CHUItable>;

export const CHUItableStory: Story = {
  name: 'CHUItable',
  args: {
    result: {
      meta: tableMeta,
      data: tableData,
      statistics: {
        elapsed: 10,
        bytes_read: 1000,
        rows_read: 100,
      },
    },
    initialPageSize: 20,
  }
}
