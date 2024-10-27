import { StoryObj, Meta } from '@storybook/react';
import ChartComponent from './ChartComponent';
import StoryBookDecorator from '@/storybook/StoryBookDecorator';

const meta: Meta<typeof ChartComponent> = {
  title: 'ChartComponent',
  component: ChartComponent,
  decorators: [
    (Story) => <StoryBookDecorator>
      <Story />
    </StoryBookDecorator>,
  ]
};

export default meta;
type Story = StoryObj<typeof ChartComponent>;

export const ChartComponentCartesianStory: Story = {
  name: 'Cartesian',
  args: {
    chartType: 'bar',
    data: [
      { date: '2021-01-01', price: 100 },
      { date: '2021-01-02', price: 200 },
      { date: '2021-01-03', price: 150 },
      { date: '2021-01-04', price: 250 },
      { date: '2021-01-05', price: 300 },
    ],
    config: {
      indexBy: 'date',
      data: {
        price: {
          label: 'Price',
          color: 'hsl(var(--chart-1))',
        }
      },
    },
    margin: { top: 20, right: 20, bottom: 20, left: 20 },
  },
  argTypes: {
    chartType: {
      control: {
        type: 'select',
      },
      options: [
        'bar',
        'line',
        'area',
      ],
    },
    data: {
      control: {
        type: 'object',
      },
    },
    config: {
      control: {
        type: 'object',
      },
    },
    margin: {
      control: {
        type: 'object',
      },
    },
  }
};

export const ChartComponentPolarStory: Story = {
  name: 'Polar',
  args: {
    chartType: 'radar',
    data: [
      { subject: 'Math', A: 120, B: 90, fullMark: 150 },
      { subject: 'Spanish', A: 75, B: 130, fullMark: 150 },
      { subject: 'English', A: 86, B: 60, fullMark: 150 },
      { subject: 'Geography', A: 99, B: 100, fullMark: 150 },
      { subject: 'Physics', A: 85, B: 40, fullMark: 150 },
      { subject: 'History', A: 65, B: 85, fullMark: 150 },
    ],
    config: {
      indexBy: 'subject',
      data: {
        A: {
          label: 'A',
          color: 'hsl(var(--chart-1))',
        },
        B: {
          label: 'B',
          color: 'hsl(var(--chart-2))',
        },
      }
    },
    margin: { top: 20, right: 20, bottom: 20, left: 20 },
  },
  argTypes: {
    chartType: {
      control: {
        type: 'select',
      },
      options: [
        'radar',
        'radial',
      ],
    },
    data: {
      control: {
        type: 'object',
      },
    },
    config: {
      control: {
        type: 'object',
      },
    },
    margin: {
      control: {
        type: 'object',
      },
    },
  }
};

export const ChartComponentPieStory: Story = {
  name: 'Pie',
  args: {
    chartType: 'pie',
    data: [
      { name: 'Group A', value: 400 },
      { name: 'Group B', value: 300 },
      { name: 'Group C', value: 300 },
      { name: 'Group D', value: 200 },
    ],
    config: {
      indexBy: 'name',
      data: {
        value: {
          label: 'Value',
          color: 'hsl(var(--chart-1))',
        }
      },
    },
    margin: { top: 20, right: 20, bottom: 20, left: 20 },
  },
  argTypes: {
    chartType: {
      control: {
        type: 'select',
      },
      options: [
        'pie',
      ],
    },
    data: {
      control: {
        type: 'object',
      },
    },
    config: {
      control: {
        type: 'object',
      },
    },
    margin: {
      control: {
        type: 'object',
      },
    },
  }
};