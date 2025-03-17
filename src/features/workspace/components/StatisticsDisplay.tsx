import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Clock, Database, FileText } from 'lucide-react';

interface StatisticsProps {
  statistics: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  } | null;
}

const StatisticsDisplay: React.FC<StatisticsProps> = ({ statistics }) => {
  if (!statistics) return null;

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 Bytes';
    
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const formatTime = (seconds: number): string => {
    if (seconds < 0.001) {
      return `${(seconds * 1000000).toFixed(2)} μs`;
    } else if (seconds < 1) {
      return `${(seconds * 1000).toFixed(2)} ms`;
    } else {
      return `${seconds.toFixed(2)} s`;
    }
  };

  const stats = [
    {
      title: 'Query Time',
      value: formatTime(statistics.elapsed),
      icon: Clock,
      description: 'Total execution time'
    },
    {
      title: 'Rows Read',
      value: statistics.rows_read.toLocaleString(),
      icon: FileText,
      description: 'Number of rows processed'
    },
    {
      title: 'Data Processed',
      value: formatBytes(statistics.bytes_read),
      icon: Database,
      description: 'Volume of data read'
    }
  ];

  return (
    <div className="grid gap-4 md:grid-cols-3">
      {stats.map((stat) => (
        <Card key={stat.title} className="overflow-hidden m-4">
          <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-sm font-medium">{stat.title}</CardTitle>
            <stat.icon className="h-4 w-4 opacity-70" />
          </CardHeader>
          <CardContent className="p-4 pt-1">
            <div className="text-2xl font-bold">{stat.value}</div>
            <p className="text-xs text-muted-foreground mt-1">{stat.description}</p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
};

export default StatisticsDisplay;

