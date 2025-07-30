pipeline {
    agent {
        node {
            label 'niva-local'
            retries 0
        }
    }
    environment {
        BINARY_NAME = 'sfp-parser'
        SERVICE_NAME = 'sfp-parser.service'
        TARGET_DIR = '/root/sfp'
    }
    stages {
        stage('Clean') {
            steps {
                script {
                    echo "Очистка предыдущей сборки..."
                    sh 'rm -f sfp-parser'
                }
            }
        }

        stage('Build') {
            steps {
                script {
                    echo "Сборка Go-проекта..."
                    sh '''
                        go mod tidy
                        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ${BINARY_NAME} .
                    '''
                    sh 'ls -la sfp-parser'  // проверка, что бинарник создан
                }
            }
        }

        stage('Deploy') {
            steps {
                script {
                    echo "Остановка сервиса перед обновлением..."
                    sh '''
                        # Останавливаем сервис (если запущен)
                        sudo systemctl stop sfp-parser.service || true
            
                        # Теперь можно безопасно копировать
                        sudo mkdir -p /root/sfp
                        sudo cp sfp-parser /root/sfp/sfp-parser
                        sudo chown root:root /root/sfp/sfp-parser
                        sudo chmod +x /root/sfp/sfp-parser
                    '''
                }
            }
        }

        stage('Restart Service') {
            steps {
                script {
                    echo "Перезапуск systemd сервиса..."
                    sh '''
                        sudo systemctl stop ${SERVICE_NAME} || true
                        sudo systemctl daemon-reload
                        sudo systemctl start ${SERVICE_NAME}
                    '''
                }
            }
        }

        stage('Verify Service') {
            steps {
                script {
                    echo "Проверка статуса сервиса..."
                    sh '''
                        sudo systemctl is-active --quiet ${SERVICE_NAME}
                        if [ $? -ne 0 ]; then
                            echo "❌ Сервис ${SERVICE_NAME} не запущен!"
                            sudo systemctl status ${SERVICE_NAME} --no-pager
                            exit 1
                        fi
                        echo "✅ Сервис ${SERVICE_NAME} работает."
                    '''
                }
            }
        }
    }

    post {
        success {
            echo "✅ Развертывание успешно завершено."
            sh 'echo "Service deployed successfully at $(date)" | logger -t jenkins'
        }
        failure {
            echo "❌ Ошибка при развертывании!"
            sh 'echo "Deployment failed at $(date)" | logger -t jenkins'
        }
    }
}
